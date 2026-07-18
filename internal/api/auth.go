package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/codegamc/home-store/internal/s3"
)

const (
	sigV4Algorithm = "AWS4-HMAC-SHA256"
	maxClockSkew   = 15 * time.Minute
)

// AuthConfig holds the single-account credentials supported by this
// single-node server. Credentials are deliberately supplied by configuration,
// never stored in the object data directory.
type AuthConfig struct {
	AccessKey string
	SecretKey string
	Region    string
	Now       func() time.Time // test seam; nil uses the current UTC time.
}

type authError struct {
	status  int
	code    string
	message string
}

var errPayloadHashMismatch = errors.New("signed payload hash does not match request body")

func (h *Handler) authorize(r *http.Request) *authError {
	if h.auth.AccessKey == "" || h.auth.SecretKey == "" || h.auth.Region == "" {
		return &authError{http.StatusForbidden, s3.AccessDenied, "server credentials are not configured"}
	}
	authorization := r.Header.Get("Authorization")
	if authorization == "" {
		return &authError{http.StatusForbidden, s3.AccessDenied, "AWS Signature Version 4 authentication is required"}
	}
	parts := strings.SplitN(authorization, " ", 2)
	if len(parts) != 2 || parts[0] != sigV4Algorithm {
		return &authError{http.StatusForbidden, s3.AccessDenied, "unsupported authorization algorithm"}
	}
	fields := make(map[string]string)
	for _, field := range strings.Split(parts[1], ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(field), "=")
		if !ok || key == "" || value == "" {
			return &authError{http.StatusForbidden, s3.AccessDenied, "malformed authorization header"}
		}
		fields[key] = value
	}
	credential, signedHeaders, signature := fields["Credential"], fields["SignedHeaders"], fields["Signature"]
	if credential == "" || signedHeaders == "" || signature == "" {
		return &authError{http.StatusForbidden, s3.AccessDenied, "incomplete authorization header"}
	}
	credentialParts := strings.Split(credential, "/")
	if len(credentialParts) != 5 || credentialParts[0] != h.auth.AccessKey || credentialParts[3] != "s3" || credentialParts[4] != "aws4_request" || credentialParts[2] != h.auth.Region {
		return &authError{http.StatusForbidden, s3.AccessDenied, "invalid access key or credential scope"}
	}
	requestTime, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
	if err != nil || credentialParts[1] != requestTime.UTC().Format("20060102") {
		return &authError{http.StatusForbidden, s3.AccessDenied, "invalid request timestamp"}
	}
	now := time.Now().UTC()
	if h.auth.Now != nil {
		now = h.auth.Now().UTC()
	}
	if requestTime.Before(now.Add(-maxClockSkew)) || requestTime.After(now.Add(maxClockSkew)) {
		return &authError{http.StatusForbidden, s3.RequestTimeTooSkewed, "request time is outside the allowed clock skew"}
	}
	canonicalHeaders, err := canonicalHeaders(r, signedHeaders)
	if err != nil {
		return &authError{http.StatusForbidden, s3.AccessDenied, err.Error()}
	}
	payloadHash := r.Header.Get("X-Amz-Content-Sha256")
	if payloadHash == "" {
		return &authError{http.StatusForbidden, s3.AccessDenied, "x-amz-content-sha256 is required"}
	}
	payloadDigest, err := hex.DecodeString(payloadHash)
	if err != nil || len(payloadDigest) != sha256.Size {
		return &authError{http.StatusForbidden, s3.AccessDenied, "invalid x-amz-content-sha256"}
	}
	canonicalRequest := strings.Join([]string{
		r.Method,
		canonicalURI(r.URL),
		canonicalQuery(r.URL.Query()),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	canonicalHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		sigV4Algorithm,
		requestTime.UTC().Format("20060102T150405Z"),
		strings.Join(credentialParts[1:], "/"),
		hex.EncodeToString(canonicalHash[:]),
	}, "\n")
	dateKey := hmacSHA256([]byte("AWS4"+h.auth.SecretKey), credentialParts[1])
	regionKey := hmacSHA256(dateKey, credentialParts[2])
	serviceKey := hmacSHA256(regionKey, credentialParts[3])
	signingKey := hmacSHA256(serviceKey, credentialParts[4])
	expected := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	provided, err := hex.DecodeString(signature)
	expectedBytes, expectedErr := hex.DecodeString(expected)
	if err != nil || expectedErr != nil || subtle.ConstantTimeCompare(expectedBytes, provided) != 1 {
		return &authError{http.StatusForbidden, s3.SignatureDoesNotMatch, "the request signature we calculated does not match the signature you provided"}
	}
	r.Body = &payloadVerifier{ReadCloser: r.Body, expected: payloadDigest}
	return nil
}

type payloadVerifier struct {
	io.ReadCloser
	hash     hash.Hash
	expected []byte
}

func (p *payloadVerifier) Read(buffer []byte) (int, error) {
	if p.hash == nil {
		p.hash = sha256.New()
	}
	n, err := p.ReadCloser.Read(buffer)
	if n > 0 {
		_, _ = p.hash.Write(buffer[:n])
	}
	if err == io.EOF && !hmac.Equal(p.hash.Sum(nil), p.expected) {
		return n, errPayloadHashMismatch
	}
	return n, err
}

func canonicalHeaders(r *http.Request, signedHeaders string) (string, error) {
	names := strings.Split(signedHeaders, ";")
	if len(names) == 0 || signedHeaders == "" {
		return "", fmt.Errorf("signed headers are required")
	}
	previous := ""
	var builder strings.Builder
	for _, name := range names {
		if name == "" || name != strings.ToLower(name) || (previous != "" && name <= previous) {
			return "", fmt.Errorf("signed headers must be lower-case and sorted")
		}
		previous = name
		var values []string
		if name == "host" {
			values = []string{r.Host}
		} else {
			values = r.Header.Values(name)
		}
		if len(values) == 0 {
			return "", fmt.Errorf("missing signed header %q", name)
		}
		for i := range values {
			values[i] = strings.Join(strings.Fields(values[i]), " ")
		}
		builder.WriteString(name)
		builder.WriteByte(':')
		builder.WriteString(strings.Join(values, ","))
		builder.WriteByte('\n')
	}
	return builder.String(), nil
}

func canonicalURI(u *url.URL) string {
	path := u.EscapedPath()
	if path == "" {
		return "/"
	}
	return path
}

func canonicalQuery(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var pairs []string
	for _, key := range keys {
		values := append([]string(nil), values[key]...)
		sort.Strings(values)
		for _, value := range values {
			pairs = append(pairs, awsURLEncode(key)+"="+awsURLEncode(value))
		}
	}
	return strings.Join(pairs, "&")
}

func awsURLEncode(value string) string {
	const hexChars = "0123456789ABCDEF"
	var builder strings.Builder
	for _, b := range []byte(value) {
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '-' || b == '_' || b == '.' || b == '~' {
			builder.WriteByte(b)
			continue
		}
		builder.WriteByte('%')
		builder.WriteByte(hexChars[b>>4])
		builder.WriteByte(hexChars[b&0x0f])
	}
	return builder.String()
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}
