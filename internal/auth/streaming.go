package auth

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type signedChunkedBody struct {
	body              io.ReadCloser
	reader            *bufio.Reader
	signingKey        []byte
	requestDate       string
	scope             string
	previousSignature string
	chunk             []byte
	done              bool
	err               error
}

func newSignedChunkedBody(body io.ReadCloser, signingKey []byte, requestDate, scope, seedSignature string) io.ReadCloser {
	return &signedChunkedBody{body: body, reader: bufio.NewReader(body), signingKey: signingKey, requestDate: requestDate, scope: scope, previousSignature: seedSignature}
}

const (
	maxChunkHeaderLength = 2048
	maxChunkSize         = 64 * 1024 * 1024
)

var (
	emptySHA256 = sha256.Sum256(nil)
)

func (b *signedChunkedBody) Read(destination []byte) (int, error) {
	if len(b.chunk) != 0 {
		count := copy(destination, b.chunk)
		b.chunk = b.chunk[count:]
		return count, nil
	}
	if b.err != nil {
		return 0, b.err
	}
	if b.done {
		return 0, io.EOF
	}
	line, err := b.readHeaderLine()
	if err != nil {
		b.err = err
		return 0, err
	}
	fields := strings.Split(line, ";")
	if len(fields[0]) == 0 {
		b.err = fmt.Errorf("invalid aws-chunked size")
		return 0, b.err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(fields[0]), 16, 64)
	if err != nil || size < 0 || size > maxChunkSize {
		b.err = fmt.Errorf("invalid aws-chunked size")
		return 0, b.err
	}
	signature := ""
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "chunk-signature=") {
			signature = strings.TrimPrefix(field, "chunk-signature=")
		}
	}
	if len(signature) != 64 {
		b.err = fmt.Errorf("missing aws-chunked signature")
		return 0, b.err
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(b.reader, data); err != nil {
		b.err = err
		return 0, err
	}
	terminator := make([]byte, 2)
	if _, err := io.ReadFull(b.reader, terminator); err != nil || string(terminator) != "\r\n" {
		b.err = fmt.Errorf("invalid aws-chunked terminator")
		return 0, b.err
	}
	dataHash := sha256.Sum256(data)
	stringToSign := "AWS4-HMAC-SHA256-PAYLOAD\n" + b.requestDate + "\n" + b.scope + "\n" + b.previousSignature + "\n" + hex.EncodeToString(emptySHA256[:]) + "\n" + hex.EncodeToString(dataHash[:])
	expected := hex.EncodeToString(hmacSHA256(b.signingKey, stringToSign))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(strings.ToLower(signature))) != 1 {
		b.err = fmt.Errorf("aws-chunked signature mismatch")
		return 0, b.err
	}
	b.previousSignature = signature
	if size == 0 {
		b.done = true
		return 0, io.EOF
	}
	b.chunk = data
	return b.Read(destination)
}

func (b *signedChunkedBody) readHeaderLine() (string, error) {
	var buf strings.Builder
	buf.Grow(128)
	for {
		if buf.Len() > maxChunkHeaderLength {
			return "", fmt.Errorf("aws-chunked header too large")
		}
		c, err := b.reader.ReadByte()
		if err != nil {
			return "", err
		}
		if c == '\n' {
			line := buf.String()
			line = strings.TrimSuffix(line, "\r")
			return line, nil
		}
		_ = buf.WriteByte(c)
	}
}

func (b *signedChunkedBody) Close() error {
	return b.body.Close()
}
