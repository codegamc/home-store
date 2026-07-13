package api

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codegamc/home-store/internal/storage"
)

func TestParseRange(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		size       int64
		start      int64
		length     int64
		partial    bool
		shouldFail bool
	}{
		{name: "full object", value: "", size: 10, start: 0, length: 10},
		{name: "closed", value: "bytes=2-5", size: 10, start: 2, length: 4, partial: true},
		{name: "open ended", value: "bytes=7-", size: 10, start: 7, length: 3, partial: true},
		{name: "suffix", value: "bytes=-3", size: 10, start: 7, length: 3, partial: true},
		{name: "suffix larger than object", value: "bytes=-30", size: 10, start: 0, length: 10, partial: true},
		{name: "end is clamped", value: "bytes=8-30", size: 10, start: 8, length: 2, partial: true},
		{name: "multiple", value: "bytes=0-1,3-4", size: 10, shouldFail: true},
		{name: "past end", value: "bytes=10-11", size: 10, shouldFail: true},
		{name: "backwards", value: "bytes=5-2", size: 10, shouldFail: true},
		{name: "zero suffix", value: "bytes=-0", size: 10, shouldFail: true},
		{name: "range of empty object", value: "bytes=0-0", size: 0, shouldFail: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, length, partial, err := parseRange(test.value, test.size)
			if test.shouldFail {
				if err == nil {
					t.Fatalf("expected %q to fail", test.value)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if start != test.start || length != test.length || partial != test.partial {
				t.Fatalf("got start=%d length=%d partial=%v", start, length, partial)
			}
		})
	}
}

func TestEvaluateReadConditions(t *testing.T) {
	modified := time.Date(2026, time.July, 10, 12, 30, 0, 0, time.UTC)
	meta := storage.ObjectMeta{ETag: `"abc"`, LastModified: modified}
	tests := []struct {
		name   string
		header http.Header
		want   int
	}{
		{name: "matching if-match", header: http.Header{"If-Match": []string{`"abc"`}}},
		{name: "if-match list", header: http.Header{"If-Match": []string{`"other", "abc"`}}},
		{name: "failed if-match", header: http.Header{"If-Match": []string{`"other"`}}, want: http.StatusPreconditionFailed},
		{name: "matching if-none-match", header: http.Header{"If-None-Match": []string{`"abc"`}}, want: http.StatusNotModified},
		{name: "wildcard if-none-match", header: http.Header{"If-None-Match": []string{"*"}}, want: http.StatusNotModified},
		{name: "modified since older", header: http.Header{"If-Modified-Since": []string{modified.Add(-time.Hour).Format(http.TimeFormat)}}},
		{name: "not modified since newer", header: http.Header{"If-Modified-Since": []string{modified.Add(time.Hour).Format(http.TimeFormat)}}, want: http.StatusNotModified},
		{name: "unmodified since older", header: http.Header{"If-Unmodified-Since": []string{modified.Add(-time.Hour).Format(http.TimeFormat)}}, want: http.StatusPreconditionFailed},
		{name: "if-match takes precedence over date", header: http.Header{
			"If-Match": []string{`"abc"`}, "If-Unmodified-Since": []string{modified.Add(-time.Hour).Format(http.TimeFormat)},
		}},
		{name: "if-none-match takes precedence over date", header: http.Header{
			"If-None-Match": []string{`"other"`}, "If-Modified-Since": []string{modified.Add(time.Hour).Format(http.TimeFormat)},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := evaluateReadConditions(meta, test.header, ""); got != test.want {
				t.Fatalf("expected status %d, got %d", test.want, got)
			}
		})
	}
}

func TestMetadataAndRequestChecksums(t *testing.T) {
	body := []byte("checksummed")
	md5Digest := md5.Sum(body)
	shaDigest := sha256.Sum256(body)
	request := httptest.NewRequest(http.MethodPut, "/bucket/key", nil)
	request.Header.Set("Content-MD5", base64.StdEncoding.EncodeToString(md5Digest[:]))
	request.Header.Set("x-amz-checksum-sha256", base64.StdEncoding.EncodeToString(shaDigest[:]))
	request.Header.Set("x-amz-meta-Color", "blue")
	request.Header.Set("Cache-Control", "max-age=60")
	meta, err := metadataFromRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ExpectedMD5 == "" || meta.ExpectedChecksumAlgorithm != "SHA256" || meta.UserMetadata["x-amz-meta-color"] != "blue" || meta.CacheControl != "max-age=60" {
		t.Fatalf("unexpected metadata: %#v", meta)
	}
	if err := verifyRequestChecksum(request, body); err != nil {
		t.Fatal(err)
	}
	if err := verifyRequestChecksum(request, []byte("tampered")); err != storage.ErrInvalidDigest {
		t.Fatalf("expected invalid digest, got %v", err)
	}
}

func TestVerifyRequestChecksumValidatesAllSupportedAlgorithms(t *testing.T) {
	body := []byte("checksummed")
	sha1Digest := sha1.Sum(body)
	sha256Digest := sha256.Sum256(body)
	for _, checksum := range []struct {
		name   string
		header string
		value  string
	}{
		{name: "crc32", header: "x-amz-checksum-crc32", value: base64.StdEncoding.EncodeToString(crc32Checksum(body, crc32.IEEETable))},
		{name: "crc32c", header: "x-amz-checksum-crc32c", value: base64.StdEncoding.EncodeToString(crc32Checksum(body, crc32.MakeTable(crc32.Castagnoli)))},
		{name: "sha1", header: "x-amz-checksum-sha1", value: base64.StdEncoding.EncodeToString(sha1Digest[:])},
		{name: "sha256", header: "x-amz-checksum-sha256", value: base64.StdEncoding.EncodeToString(sha256Digest[:])},
	} {
		t.Run(checksum.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/bucket?delete", nil)
			request.Header.Set(checksum.header, checksum.value)
			if err := verifyRequestChecksum(request, body); err != nil {
				t.Fatal(err)
			}
			if err := verifyRequestChecksum(request, []byte("tampered")); err != storage.ErrInvalidDigest {
				t.Fatalf("expected invalid digest, got %v", err)
			}
		})
	}
}

func TestS3URLEncode(t *testing.T) {
	if got, want := s3URLEncode("folder/a+b %/雪"), "folder%2Fa%2Bb%20%25%2F%E9%9B%AA"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestMultipartPaginationParameters(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
		valid bool
	}{
		{"", 1000, true}, {"1", 1, true}, {"1000", 1000, true}, {"0", 0, false}, {"1001", 0, false}, {"bad", 0, false},
	} {
		got, err := parsePositivePageSize(test.value)
		if test.valid {
			if err != nil || got != test.want {
				t.Fatalf("page size %q: got %d, err=%v", test.value, got, err)
			}
		} else if err == nil {
			t.Fatalf("page size %q unexpectedly succeeded", test.value)
		}
	}
	for _, test := range []struct {
		value string
		want  int
		valid bool
	}{
		{"", 0, true}, {"0", 0, true}, {"10000", 10000, true}, {"-1", 0, false}, {"bad", 0, false},
	} {
		got, err := parseNonNegativeMarker(test.value)
		if test.valid {
			if err != nil || got != test.want {
				t.Fatalf("marker %q: got %d, err=%v", test.value, got, err)
			}
		} else if err == nil {
			t.Fatalf("marker %q unexpectedly succeeded", test.value)
		}
	}
}

func FuzzParseRange(f *testing.F) {
	for _, seed := range []struct {
		value string
		size  uint16
	}{{"", 10}, {"bytes=0-0", 1}, {"bytes=-1", 10}, {"bytes=1-", 10}, {"bytes=0-1,3-4", 10}} {
		f.Add(seed.value, seed.size)
	}
	f.Fuzz(func(t *testing.T, value string, rawSize uint16) {
		size := int64(rawSize)
		start, length, partial, err := parseRange(value, size)
		if err != nil {
			return
		}
		if start < 0 || length < 0 || start+length > size {
			t.Fatalf("range escaped object: value=%q size=%d start=%d length=%d", value, size, start, length)
		}
		if partial && (size == 0 || length == 0) {
			t.Fatalf("empty partial range: value=%q size=%d", value, size)
		}
		if !partial && (value != "" || start != 0 || length != size) {
			t.Fatalf("invalid full range result: value=%q size=%d start=%d length=%d", value, size, start, length)
		}
	})
}
