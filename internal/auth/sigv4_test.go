package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	testAccessKey = "test-access-key"
	testSecretKey = "test-secret-key"
)

var testRequestTime = time.Date(2026, time.July, 10, 12, 30, 0, 0, time.UTC)

func TestVerifyAuthorizationHeader(t *testing.T) {
	middleware := &middleware{accessKey: testAccessKey, secretKey: testSecretKey, now: func() time.Time { return testRequestTime }}

	t.Run("accepts valid signature", func(t *testing.T) {
		request := signedHeaderRequest(t, "http://example.test/bucket/a%20b?a=2&a=1")
		code, message := middleware.verify(request)
		if code != "" || message != "" {
			t.Fatalf("expected valid signature, got %s: %s", code, message)
		}
	})

	t.Run("rejects tampered query", func(t *testing.T) {
		request := signedHeaderRequest(t, "http://example.test/bucket/key?value=original")
		request.URL.RawQuery = "value=tampered"
		code, _ := middleware.verify(request)
		if code != "SignatureDoesNotMatch" {
			t.Fatalf("expected SignatureDoesNotMatch, got %q", code)
		}
	})

	t.Run("rejects stale request", func(t *testing.T) {
		request := signedHeaderRequest(t, "http://example.test/bucket/key")
		middleware.now = func() time.Time { return testRequestTime.Add(16 * time.Minute) }
		code, _ := middleware.verify(request)
		if code != "RequestTimeTooSkewed" {
			t.Fatalf("expected RequestTimeTooSkewed, got %q", code)
		}
	})
}

func TestVerifyPresignedRequest(t *testing.T) {
	middleware := &middleware{accessKey: testAccessKey, secretKey: testSecretKey, now: func() time.Time { return testRequestTime.Add(time.Minute) }}
	request := presignedRequest(t, time.Minute)
	if code, message := middleware.verify(request); code != "" {
		t.Fatalf("expected valid presigned request, got %s: %s", code, message)
	}

	middleware.now = func() time.Time { return testRequestTime.Add(2 * time.Minute) }
	if code, _ := middleware.verify(request); code != "AccessDenied" {
		t.Fatalf("expected expired request to be denied, got %q", code)
	}
}

func TestCanonicalQuerySortsAndExcludesSignature(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://example.test/?b=two&a=2&a=1&space=a+b&X-Amz-Signature=ignored", nil)
	got := canonicalQuery(request, true)
	want := "a=1&a=2&b=two&space=a%20b"
	if got != want {
		t.Fatalf("canonical query mismatch\nwant: %s\n got: %s", want, got)
	}
}

func TestSignedChunkedBody(t *testing.T) {
	scope := "20260710/us-east-1/s3/aws4_request"
	requestDate := testRequestTime.Format("20060102T150405Z")
	key := deriveSigningKey(testSecretKey, scope)
	seed := strings.Repeat("1", 64)
	body := signedChunkStream(key, requestDate, scope, seed, []byte("hello"), []byte(" world"))
	reader := newSignedChunkedBody(io.NopCloser(bytes.NewReader(body)), key, requestDate, scope, seed)
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello world" {
		t.Fatalf("unexpected decoded body %q", got)
	}

	body[len(body)/2] ^= 1
	reader = newSignedChunkedBody(io.NopCloser(bytes.NewReader(body)), key, requestDate, scope, seed)
	if _, err := io.ReadAll(reader); err == nil {
		t.Fatal("expected a corrupt signed chunk stream to fail")
	}
}

func FuzzAWSEncode(f *testing.F) {
	for _, seed := range []string{"", "/", "a b", "snow/雪", "%2F", "a+b"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		encoded := awsEncode(value, true)
		if strings.Contains(encoded, "+") {
			t.Fatalf("AWS encoding must use %%20, not +: %q", encoded)
		}
		if encoded != awsEncode(value, true) {
			t.Fatal("AWS encoding is not deterministic")
		}
		for index := 0; index < len(encoded); index++ {
			if encoded[index] == '%' && (index+2 >= len(encoded) || !isUpperHex(encoded[index+1]) || !isUpperHex(encoded[index+2])) {
				t.Fatalf("invalid percent escape in %q", encoded)
			}
		}
	})
}

func FuzzCanonicalQuery(f *testing.F) {
	for _, seed := range []string{"", "a=1&a=2", "space=a+b", "x=%2F", "bad=%zz"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, rawQuery string) {
		if len(rawQuery) > 4096 {
			t.Skip()
		}
		request := &http.Request{URL: &url.URL{Path: "/", RawQuery: rawQuery}}
		first := canonicalQuery(request, true)
		if first != canonicalQuery(request, true) {
			t.Fatal("canonical query is not deterministic")
		}
	})
}

func signedHeaderRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	requestDate := testRequestTime.Format("20060102T150405Z")
	scope := "20260710/us-east-1/s3/aws4_request"
	empty := sha256.Sum256(nil)
	payloadHash := hex.EncodeToString(empty[:])
	request.Header.Set("x-amz-date", requestDate)
	request.Header.Set("x-amz-content-sha256", payloadHash)
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonical, err := canonicalRequest(request, signedHeaders, payloadHash, false)
	if err != nil {
		t.Fatal(err)
	}
	signature := calculateSignature(testSecretKey, scope, requestDate, canonical)
	request.Header.Set("Authorization", fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s", algorithm, testAccessKey, scope, signedHeaders, signature))
	return request
}

func presignedRequest(t *testing.T, expires time.Duration) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://example.test/bucket/key", nil)
	requestDate := testRequestTime.Format("20060102T150405Z")
	scope := "20260710/us-east-1/s3/aws4_request"
	query := request.URL.Query()
	query.Set("X-Amz-Algorithm", algorithm)
	query.Set("X-Amz-Credential", testAccessKey+"/"+scope)
	query.Set("X-Amz-Date", requestDate)
	query.Set("X-Amz-Expires", fmt.Sprintf("%.0f", expires.Seconds()))
	query.Set("X-Amz-SignedHeaders", "host")
	request.URL.RawQuery = query.Encode()
	canonical, err := canonicalRequest(request, "host", "UNSIGNED-PAYLOAD", true)
	if err != nil {
		t.Fatal(err)
	}
	query.Set("X-Amz-Signature", calculateSignature(testSecretKey, scope, requestDate, canonical))
	request.URL.RawQuery = query.Encode()
	return request
}

func signedChunkStream(key []byte, requestDate, scope, seed string, chunks ...[]byte) []byte {
	var result bytes.Buffer
	previous := seed
	for _, chunk := range append(chunks, nil) {
		emptyHash := sha256.Sum256(nil)
		dataHash := sha256.Sum256(chunk)
		toSign := "AWS4-HMAC-SHA256-PAYLOAD\n" + requestDate + "\n" + scope + "\n" + previous + "\n" + hex.EncodeToString(emptyHash[:]) + "\n" + hex.EncodeToString(dataHash[:])
		signature := hex.EncodeToString(hmacSHA256(key, toSign))
		_, _ = fmt.Fprintf(&result, "%x;chunk-signature=%s\r\n", len(chunk), signature)
		_, _ = result.Write(chunk)
		_, _ = result.WriteString("\r\n")
		previous = signature
	}
	return result.Bytes()
}

func isUpperHex(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'A' && value <= 'F'
}
