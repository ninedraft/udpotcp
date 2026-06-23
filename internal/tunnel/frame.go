package tunnel

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const MaxDatagramSize = 65507

var ErrDatagramTooLarge = errors.New("datagram too large")

func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > MaxDatagramSize {
		return fmt.Errorf("%w: %d > %d", ErrDatagramTooLarge, len(payload), MaxDatagramSize)
	}

	var header [2]byte
	binary.BigEndian.PutUint16(header[:], uint16(len(payload)))
	if err := writeAll(w, header[:]); err != nil {
		return fmt.Errorf("writing frame length: %w", err)
	}
	if err := writeAll(w, payload); err != nil {
		return fmt.Errorf("writing frame payload: %w", err)
	}
	return nil
}

func ReadFrame(r io.Reader, buf []byte) ([]byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}

	n := int(binary.BigEndian.Uint16(header[:]))
	if n > MaxDatagramSize {
		return nil, fmt.Errorf("%w: %d > %d", ErrDatagramTooLarge, n, MaxDatagramSize)
	}
	if len(buf) < n {
		buf = make([]byte, n)
	}
	if _, err := io.ReadFull(r, buf[:n]); err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}
