package gbufio

import (
	"errors"
	"io"

	"github.com/zalgonoise/cfg"
	"github.com/zalgonoise/gio"
)

var errNegativeWrite = errors.New("gbufio: writer returned negative count from Write")

// Writer implements buffering for an io.Writer object.
// If an error occurs writing to a Writer, no more data will be
// accepted and all subsequent writes, and Flush, will return the error.
// After all data has been written, the client should call the
// Flush method to guarantee all data has been forwarded to
// the underlying io.Writer.
type Writer[T any] struct {
	err error
	buf []T
	n   int
	wr  gio.Writer[T]
}

// NewWriterSize returns a new Writer whose buffer has at least the specified
// size. If the argument io.Writer is already a Writer with large enough
// size, it returns the underlying Writer.
func newWriter[T any](w gio.Writer[T], size int) *Writer[T] {
	// Is it already a Writer?
	b, ok := w.(*Writer[T])
	if ok && len(b.buf) >= size {
		return b
	}
	if size <= 0 {
		size = defaultBufSize
	}
	return &Writer[T]{
		buf: make([]T, size),
		wr:  w,
	}
}

// NewWriter returns a new Writer whose buffer has the default size.
// If the argument gio.Writer is already a Writer with large enough buffer size,
// it returns the underlying Writer.
func NewWriter[T any](w gio.Writer[T], opts ...cfg.Option[Config[T]]) *Writer[T] {
	config := cfg.Set(defaultConfig[T](), opts...)

	return newWriter(w, config.size)
}

// Size returns the size of the underlying buffer.
func (b *Writer[T]) Size() int { return len(b.buf) }

// Reset discards any unflushed buffered data, clears any error, and
// resets b to write its output to w.
// Calling Reset on the zero value of Writer initializes the internal buffer
// to the default size.
// Calling w.Reset(w) (that is, resetting a Writer to itself) does nothing.
func (b *Writer[T]) Reset(w gio.Writer[T]) {
	// If a Writer w is passed to NewWriter, NewWriter will return w.
	// Different layers of code may do that, and then later pass w
	// to Reset. Avoid infinite recursion in that case.
	if b == w {
		return
	}
	if b.buf == nil {
		b.buf = make([]T, defaultBufSize)
	}
	b.err = nil
	b.n = 0
	b.wr = w
}

// Flush writes any buffered data to the underlying gio.Writer.
func (b *Writer[T]) Flush() error {
	if b.err != nil {
		return b.err
	}
	if b.n == 0 {
		return nil
	}
	n, err := b.wr.Write(b.buf[0:b.n])
	if n < b.n && err == nil {
		err = io.ErrShortWrite
	}
	if err != nil {
		if n > 0 && n < b.n {
			copy(b.buf[0:b.n-n], b.buf[n:b.n])
		}
		b.n -= n
		b.err = err
		return err
	}
	b.n = 0
	return nil
}

// Available returns how many items are unused in the buffer.
func (b *Writer[T]) Available() int { return len(b.buf) - b.n }

// AvailableBuffer returns an empty buffer with b.Available() capacity.
// This buffer is intended to be appended to and
// passed to an immediately succeeding Write call.
// The buffer is only valid until the next write operation on b.
func (b *Writer[T]) AvailableBuffer() []T {
	return b.buf[b.n:][:0]
}

// Buffered returns the number of item that have been written into the current buffer.
func (b *Writer[T]) Buffered() int { return b.n }

// Write writes the contents of p into the buffer.
// It returns the number of bytes written.
// If nn < len(p), it also returns an error explaining
// why the write is short.
func (b *Writer[T]) Write(p []T) (nn int, err error) {
	for len(p) > b.Available() && b.err == nil {
		var n int
		if b.Buffered() == 0 {
			// Large write, empty buffer.
			// Write directly from p to avoid copy.
			n, b.err = b.wr.Write(p)
		} else {
			n = copy(b.buf[b.n:], p)
			b.n += n
			b.Flush()
		}
		nn += n
		p = p[n:]
	}
	if b.err != nil {
		return nn, b.err
	}
	n := copy(b.buf[b.n:], p)
	b.n += n
	nn += n
	return nn, nil
}

// WriteItem writes a single item.
func (b *Writer[T]) WriteItem(c T) error {
	if b.err != nil {
		return b.err
	}
	if b.Available() <= 0 && b.Flush() != nil {
		return b.err
	}
	b.buf[b.n] = c
	b.n++
	return nil
}

// ReadFrom implements io.ReaderFrom. If the underlying writer
// supports the ReadFrom method, this calls the underlying ReadFrom.
// If there is buffered data and an underlying ReadFrom, this fills
// the buffer and writes it before calling ReadFrom.
func (b *Writer[T]) ReadFrom(r gio.Reader[T]) (n int64, err error) {
	if b.err != nil {
		return 0, b.err
	}
	readerFrom, readerFromOK := b.wr.(gio.ReaderFrom[T])
	var m int
	for {
		if b.Available() == 0 {
			if err1 := b.Flush(); err1 != nil {
				return n, err1
			}
		}
		if readerFromOK && b.Buffered() == 0 {
			nn, err := readerFrom.ReadFrom(r)
			b.err = err
			n += nn
			return n, err
		}
		nr := 0
		for nr < maxConsecutiveEmptyReads {
			m, err = r.Read(b.buf[b.n:])
			if m != 0 || err != nil {
				break
			}
			nr++
		}
		if nr == maxConsecutiveEmptyReads {
			return n, io.ErrNoProgress
		}
		b.n += m
		n += int64(m)
		if err != nil {
			break
		}
	}
	if err == io.EOF {
		// If we filled the buffer exactly, flush preemptively.
		if b.Available() == 0 {
			err = b.Flush()
		} else {
			err = nil
		}
	}
	return n, err
}
