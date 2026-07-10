package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const algorithm = "AWS4-HMAC-SHA256"

type middleware struct {
	accessKey string
	secretKey string
	disabled  bool
	next      http.Handler
	now       func() time.Time
}

func New(accessKey, secretKey string, disabled bool, next http.Handler) http.Handler {
	return &middleware{accessKey: accessKey, secretKey: secretKey, disabled: disabled, next: next, now: time.Now}
}

func (m *middleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if m.disabled || r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
		m.next.ServeHTTP(w, r)
		return
	}
	if code, message := m.verify(r); code != "" {
		writeAuthError(w, r, code, message)
		return
	}
	m.next.ServeHTTP(w, r)
}

func (m *middleware) verify(r *http.Request) (string, string) {
	if r.URL.Query().Get("X-Amz-Algorithm") != "" {
		return m.verifyPresigned(r)
	}
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, algorithm+" ") {
		return "AccessDenied", "AWS Signature Version 4 authentication is required"
	}
	fields := parseAuthorizationFields(strings.TrimPrefix(value, algorithm+" "))
	credential, signedHeaders, signature := fields["Credential"], fields["SignedHeaders"], fields["Signature"]
	if credential == "" || signedHeaders == "" || signature == "" {
		return "AuthorizationHeaderMalformed", "the authorization header is malformed"
	}
	access, scope, ok := parseCredential(credential)
	if !ok {
		return "AuthorizationHeaderMalformed", "the credential scope is malformed"
	}
	if subtle.ConstantTimeCompare([]byte(access), []byte(m.accessKey)) != 1 {
		return "InvalidAccessKeyId", "the AWS access key ID you provided does not exist"
	}
	requestTime, err := time.Parse("20060102T150405Z", r.Header.Get("x-amz-date"))
	if err != nil {
		return "AccessDenied", "x-amz-date is missing or invalid"
	}
	if delta := m.now().UTC().Sub(requestTime); delta > 15*time.Minute || delta < -15*time.Minute {
		return "RequestTimeTooSkewed", "the difference between the request time and the current time is too large"
	}
	if !validScope(scope, requestTime) {
		return "AuthorizationHeaderMalformed", "credential scope must target the s3 service"
	}
	payloadHash := r.Header.Get("x-amz-content-sha256")
	if payloadHash == "" {
		empty := sha256.Sum256(nil)
		payloadHash = hex.EncodeToString(empty[:])
	}
	canonical, err := canonicalRequest(r, signedHeaders, payloadHash, false)
	if err != nil {
		return "AuthorizationHeaderMalformed", err.Error()
	}
	expected := calculateSignature(m.secretKey, scope, r.Header.Get("x-amz-date"), canonical)
	if !secureEqualHex(expected, signature) {
		return "SignatureDoesNotMatch", "the request signature we calculated does not match the signature you provided"
	}
	if payloadHash == "STREAMING-AWS4-HMAC-SHA256-PAYLOAD" {
		r.Body = newSignedChunkedBody(r.Body, deriveSigningKey(m.secretKey, scope), r.Header.Get("x-amz-date"), scope, signature)
		r.Header.Del("Content-Encoding")
		if decodedLength, err := strconv.ParseInt(r.Header.Get("x-amz-decoded-content-length"), 10, 64); err == nil {
			r.ContentLength = decodedLength
		} else {
			r.ContentLength = -1
		}
	}
	return "", ""
}

func (m *middleware) verifyPresigned(r *http.Request) (string, string) {
	query := r.URL.Query()
	if query.Get("X-Amz-Algorithm") != algorithm {
		return "AuthorizationQueryParametersError", "unsupported signing algorithm"
	}
	access, scope, ok := parseCredential(query.Get("X-Amz-Credential"))
	if !ok {
		return "AuthorizationQueryParametersError", "credential scope is malformed"
	}
	if subtle.ConstantTimeCompare([]byte(access), []byte(m.accessKey)) != 1 {
		return "InvalidAccessKeyId", "the AWS access key ID you provided does not exist"
	}
	requestTime, err := time.Parse("20060102T150405Z", query.Get("X-Amz-Date"))
	if err != nil || !validScope(scope, requestTime) {
		return "AuthorizationQueryParametersError", "date or credential scope is invalid"
	}
	expires, err := strconv.ParseInt(query.Get("X-Amz-Expires"), 10, 64)
	if err != nil || expires < 0 || expires > 7*24*60*60 {
		return "AuthorizationQueryParametersError", "X-Amz-Expires must be between 0 and 604800 seconds"
	}
	now := m.now().UTC()
	if now.Before(requestTime.Add(-15 * time.Minute)) {
		return "RequestTimeTooSkewed", "the request date is too far in the future"
	}
	if now.After(requestTime.Add(time.Duration(expires) * time.Second)) {
		return "AccessDenied", "request has expired"
	}
	payloadHash := r.Header.Get("x-amz-content-sha256")
	if payloadHash == "" {
		payloadHash = "UNSIGNED-PAYLOAD"
	}
	canonical, err := canonicalRequest(r, query.Get("X-Amz-SignedHeaders"), payloadHash, true)
	if err != nil {
		return "AuthorizationQueryParametersError", err.Error()
	}
	expected := calculateSignature(m.secretKey, scope, query.Get("X-Amz-Date"), canonical)
	if !secureEqualHex(expected, query.Get("X-Amz-Signature")) {
		return "SignatureDoesNotMatch", "the request signature we calculated does not match the signature you provided"
	}
	return "", ""
}

func parseAuthorizationFields(value string) map[string]string {
	result := map[string]string{}
	for _, field := range strings.Split(value, ",") {
		parts := strings.SplitN(strings.TrimSpace(field), "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

func parseCredential(value string) (access, scope string, ok bool) {
	parts := strings.Split(value, "/")
	if len(parts) != 5 || parts[0] == "" {
		return "", "", false
	}
	return parts[0], strings.Join(parts[1:], "/"), true
}

func validScope(scope string, requestTime time.Time) bool {
	parts := strings.Split(scope, "/")
	return len(parts) == 4 && parts[0] == requestTime.UTC().Format("20060102") && parts[1] != "" && parts[2] == "s3" && parts[3] == "aws4_request"
}

func canonicalRequest(r *http.Request, signedHeaders, payloadHash string, presigned bool) (string, error) {
	headerNames := strings.Split(signedHeaders, ";")
	if len(headerNames) == 0 || signedHeaders == "" {
		return "", fmt.Errorf("signed headers are missing")
	}
	canonicalHeaders := strings.Builder{}
	for _, name := range headerNames {
		if name == "" || name != strings.ToLower(name) {
			return "", fmt.Errorf("signed header names must be lowercase")
		}
		value := ""
		if name == "host" {
			value = r.Host
		} else if name == "content-length" && r.ContentLength >= 0 {
			value = strconv.FormatInt(r.ContentLength, 10)
		} else {
			values, found := r.Header[http.CanonicalHeaderKey(name)]
			if !found {
				return "", fmt.Errorf("signed header %q is missing", name)
			}
			value = strings.Join(values, ",")
		}
		canonicalHeaders.WriteString(name)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(normalizeHeaderValue(value))
		canonicalHeaders.WriteByte('\n')
	}
	return strings.Join([]string{
		r.Method,
		awsEncodePath(r.URL.Path),
		canonicalQuery(r, presigned),
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n"), nil
}

func canonicalQuery(r *http.Request, excludeSignature bool) string {
	query := r.URL.Query()
	keys := make([]string, 0, len(query))
	for key := range query {
		if excludeSignature && strings.EqualFold(key, "X-Amz-Signature") {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var values []string
	for _, key := range keys {
		encodedKey := awsEncode(key, true)
		encodedValues := append([]string(nil), query[key]...)
		sort.Strings(encodedValues)
		for _, value := range encodedValues {
			values = append(values, encodedKey+"="+awsEncode(value, true))
		}
	}
	return strings.Join(values, "&")
}

func awsEncodePath(path string) string {
	if path == "" {
		return "/"
	}
	return awsEncode(path, false)
}

func awsEncode(value string, encodeSlash bool) string {
	const hexChars = "0123456789ABCDEF"
	var result strings.Builder
	for i := 0; i < len(value); i++ {
		character := value[i]
		if (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' || character == '~' || (!encodeSlash && character == '/') {
			result.WriteByte(character)
			continue
		}
		result.WriteByte('%')
		result.WriteByte(hexChars[character>>4])
		result.WriteByte(hexChars[character&15])
	}
	return result.String()
}

func normalizeHeaderValue(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func calculateSignature(secret, scope, requestDate, canonical string) string {
	canonicalHash := sha256.Sum256([]byte(canonical))
	stringToSign := algorithm + "\n" + requestDate + "\n" + scope + "\n" + hex.EncodeToString(canonicalHash[:])
	signingKey := deriveSigningKey(secret, scope)
	return hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
}

func deriveSigningKey(secret, scope string) []byte {
	parts := strings.Split(scope, "/")
	dateKey := hmacSHA256([]byte("AWS4"+secret), parts[0])
	regionKey := hmacSHA256(dateKey, parts[1])
	serviceKey := hmacSHA256(regionKey, parts[2])
	return hmacSHA256(serviceKey, parts[3])
}

func hmacSHA256(key []byte, value string) []byte {
	hash := hmac.New(sha256.New, key)
	_, _ = hash.Write([]byte(value))
	return hash.Sum(nil)
}

func secureEqualHex(expected, actual string) bool {
	if len(expected) != len(actual) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(strings.ToLower(actual))) == 1
}

type authError struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource"`
	RequestID string   `xml:"RequestId"`
}

func writeAuthError(w http.ResponseWriter, r *http.Request, code, message string) {
	status := http.StatusForbidden
	if code == "AuthorizationHeaderMalformed" || code == "AuthorizationQueryParametersError" {
		status = http.StatusBadRequest
	}
	idBytes := make([]byte, 16)
	_, _ = rand.Read(idBytes)
	id := hex.EncodeToString(idBytes)
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("x-amz-request-id", id)
	w.WriteHeader(status)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(authError{Code: code, Message: message, Resource: r.URL.Path, RequestID: id})
}
