package gbufio

import (
	"errors"
	"slices"

	"github.com/zalgonoise/cfg"
	"github.com/zalgonoise/gio"
)

type ComparableReader[T comparable] struct {
	*Reader[T]

	delim    T
	excluded *T
}

// NewComparableReader returns a new Reader for types that allow comparisons, unlocking added features to work
// with this data.
//
// The ComparableReader type can be configured with a set size (via WithSize) and delimiter (via WithDelim).
func NewComparableReader[T comparable](rd gio.Reader[T], opts ...cfg.Option[Config[T]]) *ComparableReader[T] {
	config := cfg.Set(defaultConfig[T](), opts...)

	return newComparableReader(rd, config.size, config.delim)
}

func newComparableReader[T comparable](rd gio.Reader[T], size int, delim T) *ComparableReader[T] {
	switch r := rd.(type) {
	case *ComparableReader[T]:
		return r
	case *Reader[T]:
		return &ComparableReader[T]{Reader: r, delim: delim}
	}

	// Is it already a Reader?
	b, ok := rd.(*Reader[T])
	if ok && len(b.buf) >= size {
		return &ComparableReader[T]{Reader: b}
	}

	if size < minReadBufferSize {
		size = minReadBufferSize
	}

	r := new(ComparableReader[T])
	r.reset(make([]T, size), rd)
	return r
}

// ReadSlice reads until the first occurrence of delim in the input,
// returning a slice pointing at the items in the buffer.
// The items stop being valid at the next read.
// If ReadSlice encounters an error before finding a delimiter,
// it returns all the data in the buffer and the error itself (often io.EOF).
// ReadSlice fails with error ErrBufferFull if the buffer fills without a delim.
// Because the data returned from ReadSlice will be overwritten
// by the next I/O operation, most clients should use Read or ReadItem instead.
// ReadSlice returns err != nil if and only if line does not end in delim.
func (b *ComparableReader[T]) ReadSlice(delim T) (line []T, err error) {

	s := 0 // search start index
	for {
		// Search buffer.
		if i := slices.Index(b.buf[b.r+s:b.w], delim); i >= 0 {
			i += s
			line = b.buf[b.r : b.r+i+1]
			b.r += i + 1
			break
		}

		// Pending error?
		if b.err != nil {
			line = b.buf[b.r:b.w]
			b.r = b.w
			err = b.readErr()
			break
		}

		// Buffer full?
		if b.Buffered() >= len(b.buf) {
			b.r = b.w
			line = b.buf
			err = ErrBufferFull
			break
		}

		s = b.w - b.r // do not rescan area we scanned before

		b.fill() // buffer is not full
	}

	// Handle last byte, if any.
	if i := len(line) - 1; i >= 0 {
		b.lastItem = &line[i]
	}

	return
}

// ReadLine is a low-level line-reading primitive. Most callers should use
// ReadBytes('\n') or ReadString('\n') instead or use a Scanner.
//
// ReadLine tries to return a single line, not including the end-of-line bytes.
// If the line was too long for the buffer then isPrefix is set and the
// beginning of the line is returned. The rest of the line will be returned
// from future calls. isPrefix will be false when returning the last fragment
// of the line. The returned buffer is only valid until the next call to
// ReadLine. ReadLine either returns a non-nil line or it returns an error,
// never both.
//
// The text returned from ReadLine does not include the line end ("\r\n" or "\n").
// No indication or error is given if the input ends without a final line end.
// Calling UnreadByte after ReadLine will always unread the last byte read
// (possibly a character belonging to the line end) even if that byte is not
// part of the line returned by ReadLine.
func (b *ComparableReader[T]) ReadLine() (line []T, isPrefix bool, err error) {
	line, err = b.ReadSlice(b.delim)
	if errors.Is(err, ErrBufferFull) {
		// Handle the case where the excluded item (e.g. "\r\n") straddles the buffer.
		if len(line) > 0 && b.excluded != nil && line[len(line)-1] == *b.excluded {
			// Put the excluded item (e.g. '\r') back on buf and drop it from line.
			// Let the next call to ReadLine check for the excluded item plus delimiter (e.g. "\r\n").
			if b.r == 0 {
				// should be unreachable
				panic("bufio: tried to rewind past start of buffer")
			}
			b.r--
			line = line[:len(line)-1]
		}
		return line, true, nil
	}

	if len(line) == 0 {
		if err != nil {
			line = nil
		}
		return
	}
	err = nil

	if line[len(line)-1] == b.delim {
		drop := 1
		if len(line) > 1 && b.excluded != nil && line[len(line)-2] == *b.excluded {
			drop = 2
		}
		line = line[:len(line)-drop]
	}
	return
}

// collectFragments reads until the first occurrence of delim in the input. It
// returns (slice of full buffers, remaining bytes before delim, total number
// of bytes in the combined first two elements, error).
// The complete result is equal to
// `bytes.Join(append(fullBuffers, finalFragment), nil)`, which has a
// length of `totalLen`. The result is structured in this way to allow callers
// to minimize allocations and copies.
func (b *ComparableReader[T]) collectFragments(delim T) (fullBuffers [][]T, finalFragment []T, totalLen int, err error) {
	var frag []T
	// Use ReadSlice to look for delim, accumulating full buffers.
	for {
		var e error
		frag, e = b.ReadSlice(delim)
		if e == nil { // got final fragment
			break
		}
		if !errors.Is(e, ErrBufferFull) { // unexpected error
			err = e
			break
		}

		// Make a copy of the buffer.
		buf := make([]T, len(frag))
		copy(buf, frag)
		fullBuffers = append(fullBuffers, buf)
		totalLen += len(buf)
	}

	totalLen += len(frag)
	return fullBuffers, frag, totalLen, err
}

// ReadItems reads until the first occurrence of delim in the input,
// returning a slice containing the data up to and including the delimiter.
// If ReadBytes encounters an error before finding a delimiter,
// it returns the data read before the error and the error itself (often io.EOF).
// ReadBytes returns err != nil if and only if the returned data does not end in
// delim.
// For simple uses, a Scanner may be more convenient.
func (b *ComparableReader[T]) ReadItems(delim T) ([]T, error) {
	full, frag, n, err := b.collectFragments(delim)
	// Allocate new buffer to hold the full pieces and the fragment.
	buf := make([]T, n)
	n = 0
	// Copy full pieces and fragment in.
	for i := range full {
		n += copy(buf[n:], full[i])
	}
	copy(buf[n:], frag)
	return buf, err
}
