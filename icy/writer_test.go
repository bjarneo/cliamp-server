package icy

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuildMetaCapsPayloadAtProtocolLimit(t *testing.T) {
	block := BuildMeta(strings.Repeat("x", 5000))
	if len(block) != 1+maxMetaPayload {
		t.Fatalf("BuildMeta block length = %d, want %d", len(block), 1+maxMetaPayload)
	}
	if block[0] != 255 {
		t.Fatalf("BuildMeta length byte = %d, want 255", block[0])
	}
}

func TestBuildMetaTruncatesAtUTF8Boundary(t *testing.T) {
	block := BuildMeta(strings.Repeat("é", 3000))
	payload := bytes.TrimRight(block[1:], "\x00")
	if !utf8.Valid(payload) {
		t.Fatal("BuildMeta split a UTF-8 sequence")
	}
}

func TestWriterUsesNewMetadataAtExactBoundary(t *testing.T) {
	var output bytes.Buffer
	writer := NewWriter(&output, 4)
	writer.SetMeta("Old")
	if n, err := writer.Write([]byte("aaaa")); err != nil || n != 4 {
		t.Fatalf("first Write = (%d, %v), want (4, nil)", n, err)
	}
	if got := output.String(); got != "aaaa" {
		t.Fatalf("output after exact boundary = %q, want audio only", got)
	}

	writer.SetMeta("New")
	if n, err := writer.Write([]byte("b")); err != nil || n != 1 {
		t.Fatalf("second Write = (%d, %v), want (1, nil)", n, err)
	}
	want := append([]byte("aaaa"), BuildMeta("New")...)
	want = append(want, 'b')
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatal("writer emitted stale metadata at an exact track boundary")
	}
}

func TestWriterCompletesShortWrites(t *testing.T) {
	underlying := &shortWriter{max: 2}
	writer := NewWriter(underlying, 100)
	input := []byte("abcdef")
	n, err := writer.Write(input)
	if err != nil || n != len(input) {
		t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(input))
	}
	if !bytes.Equal(underlying.Bytes(), input) {
		t.Fatalf("underlying data = %q, want %q", underlying.Bytes(), input)
	}
}

func TestWriterRejectsNoProgress(t *testing.T) {
	writer := NewWriter(zeroWriter{}, 100)
	if _, err := writer.Write([]byte("audio")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Write error = %v, want io.ErrShortWrite", err)
	}
}

type shortWriter struct {
	bytes.Buffer
	max int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > w.max {
		p = p[:w.max]
	}
	return w.Buffer.Write(p)
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) {
	return 0, nil
}
