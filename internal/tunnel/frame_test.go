package tunnel

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

type oneByteReader struct {
	r *bytes.Reader
}

func (r oneByteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.r.Read(p)
}

func TestWriteFramePrefixesPayloadWithBigEndianLength(t *testing.T) {
	var out bytes.Buffer

	if err := WriteFrame(&out, []byte("hello")); err != nil {
		t.Fatalf("WriteFrame() error = %v", err)
	}

	want := append([]byte{0, 5}, []byte("hello")...)
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("frame bytes = %v, want %v", out.Bytes(), want)
	}
}

func TestReadFramePreservesDatagramBoundariesAcrossPartialReads(t *testing.T) {
	var stream bytes.Buffer
	if err := binary.Write(&stream, binary.BigEndian, uint16(3)); err != nil {
		t.Fatal(err)
	}
	stream.WriteString("abc")
	if err := binary.Write(&stream, binary.BigEndian, uint16(4)); err != nil {
		t.Fatal(err)
	}
	stream.WriteString("defg")

	buf := make([]byte, MaxDatagramSize)
	first, err := ReadFrame(oneByteReader{r: bytes.NewReader(stream.Bytes())}, buf)
	if err != nil {
		t.Fatalf("ReadFrame(first) error = %v", err)
	}
	if string(first) != "abc" {
		t.Fatalf("first frame = %q, want %q", first, "abc")
	}
}

func TestReadFrameReturnsEOFOnlyBetweenFrames(t *testing.T) {
	_, err := ReadFrame(strings.NewReader(""), make([]byte, MaxDatagramSize))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadFrame(empty) error = %v, want io.EOF", err)
	}

	var truncated bytes.Buffer
	truncated.Write([]byte{0, 4})
	truncated.WriteString("xy")
	_, err = ReadFrame(&truncated, make([]byte, MaxDatagramSize))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ReadFrame(truncated) error = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestWriteFrameRejectsOversizedDatagram(t *testing.T) {
	err := WriteFrame(io.Discard, make([]byte, MaxDatagramSize+1))
	if !errors.Is(err, ErrDatagramTooLarge) {
		t.Fatalf("WriteFrame(oversized) error = %v, want ErrDatagramTooLarge", err)
	}
}

func TestWriteFrameRetriesShortWrites(t *testing.T) {
	w := shortWriter{limit: 1}

	if err := WriteFrame(&w, []byte("ok")); err != nil {
		t.Fatalf("WriteFrame() error = %v", err)
	}

	want := []byte{0, 2, 'o', 'k'}
	if !bytes.Equal(w.buf.Bytes(), want) {
		t.Fatalf("written bytes = %v, want %v", w.buf.Bytes(), want)
	}
}

type shortWriter struct {
	limit int
	buf   bytes.Buffer
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit {
		p = p[:w.limit]
	}
	return w.buf.Write(p)
}
