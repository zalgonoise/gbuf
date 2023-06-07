package gbuf

import (
	"errors"
	"io"

	"github.com/zalgonoise/gio"
)

// RingFilter is a buffer that is connected end-to-end, which allows continuous
// reads and writes where the caller configures a callback function to process
// all items on each loop
type RingFilter[T any] struct {
	write int
	read  int
	fn    func([]T) error
	items []T
}

// Write sets the contents of `p` to the buffer, in sequential order.
// The return value n is the length of p; err comes from the process func.
// If the index in the buffer has not been yet read, the entire unread
// buffer value is sent to the configured process function
func (r *RingFilter[T]) Write(p []T) (n int, err error) {
	var (
		ln     = len(p)
		ringLn = r.Len()
	)

	if ln < ringLn {
		n = copy(r.items[r.write:r.write+ln], p)

		err = r.fn(r.items[r.write : r.write+ln])
		if err != nil {
			return n, err
		}

		r.write += ln

		return n, nil
	}

	for n < ln {
		if r.write > 0 {
			ringLn = r.Len()
			if n+ringLn > ln {
				ringLn -= (n + ringLn) % ln
			}

			n += copy(r.items[r.write:r.Cap()], p[n:n+ringLn])

			err = r.fn(r.items[r.write:r.Cap()])
			if err != nil {
				return n, err
			}

			// buffer is reset, can fill from 0:read again
			r.write = 0

			continue
		}

		ringLn = r.Cap()
		if n+ringLn > ln {
			ringLn -= (n + ringLn) % ln
		}

		n += copy(r.items[0:ringLn], p[n:n+ringLn])

		err = r.fn(r.items[0:ringLn])
		if err != nil {
			return n, err
		}
	}

	return n, nil
}

// WriteTo writes data to w until the buffer is drained or an error occurs.
// The return value n is the number of T items written; it always fits into an
// int, but it is int64 to match the gio.WriterTo interface. Any error
// encountered during the write is also returned.
func (r *RingFilter[T]) WriteTo(b gio.Writer[T]) (n int64, err error) {
	for {
		if r.write < r.read {
			num, err := b.Write(r.items[r.write:r.read])
			if n < 0 {
				panic(errNegativeRead)
			}
			n += int64(num)
			if errors.Is(err, io.EOF) {
				return n, nil
			}
			if err != nil {
				return n, err
			}
		} else {
			num, err := b.Write(r.items[r.write:len(r.items)])
			if n < 0 {
				panic(errNegativeRead)
			}
			n += int64(num)
			if errors.Is(err, io.EOF) {
				return n, nil
			}
			if err != nil {
				return n, err
			}
			num, err = b.Write(r.items[:r.read])
			if n < 0 {
				panic(errNegativeRead)
			}
			n += int64(num)
			if errors.Is(err, io.EOF) {
				r.Reset()
				return n, nil
			}
			if err != nil {
				return n, err
			}
		}
	}
}

// Reset resets the buffer to be empty,
// but it retains the underlying storage for use by future writes.
func (r *RingFilter[T]) Reset() {
	r.write = 0
	r.read = 0
}

// Read reads the next len(p) T items from the buffer or until the buffer
// is drained. The return value n is the number of T items read. If the
// buffer has no data to return, err is io.EOF (unless len(p) is zero);
// otherwise it is nil.
func (r *RingFilter[T]) Read(p []T) (n int, err error) {
	if r.write == r.read {
		return 0, io.EOF
	}
	if r.write < r.read {
		n = copy(p, r.items[r.write:r.read])
	} else {
		n = copy(p, r.items[r.write-1:])
		if n < len(p) {
			n += copy(p[n:], r.items[:r.read])
		}
	}
	r.write = (r.write + n) % len(r.items)
	return n, nil
}

// ReadFrom reads data from b until EOF and appends it to the buffer, cycling
// the buffer as needed. For each complete cycle, the process function is called
// with the full buffer in the ring, and the ring reset.
// The return value n is the number of T items read. Any error except io.EOF
// encountered during the read is also returned.
func (r *RingFilter[T]) ReadFrom(b gio.Reader[T]) (n int64, err error) {

	var num int
	for {
		num, err = b.Read(r.items[r.read:len(r.items)])
		if n < 0 {
			panic(errNegativeRead)
		}
		switch {
		case errors.Is(err, io.EOF):
			r.read = (r.read + num) % len(r.items)
			err = r.fn(r.items[r.write:len(r.items)])
			if err != nil {
				return n, err
			}
			return n, nil
		case err != nil:
			return n, err
		}

		r.read = (r.read + num) % len(r.items)
		n += int64(num)
		if r.read == r.write {
			err = r.fn(r.items[r.write:len(r.items)])
			if err != nil {
				return n, err
			}
			r.Reset()
		}
		if err != nil {
			return n, err
		}
	}
}

// ReadFromIf extends ReadFrom with a conditional function that is called on each loop.
// If the condition(s) is / are met, the loop will continue.
// The return value n is the number of T items read. Any error except io.EOF
// encountered during the read is also returned.
func (r *RingFilter[T]) ReadFromIf(b gio.Reader[T], cond func() bool) (n int64, err error) {
	for cond() {
		num, err := b.Read(r.items[r.read:len(r.items)])
		if n < 0 {
			panic(errNegativeRead)
		}
		if errors.Is(err, io.EOF) {
			r.read = (r.read + num) % len(r.items)
			err = r.fn(r.items[r.write:len(r.items)])
			if err != nil {
				return n, err
			}
			return n, nil
		}
		r.read = (r.read + num) % len(r.items)
		n += int64(num)
		if r.read == r.write {
			err = r.fn(r.items[r.write:len(r.items)])
			if err != nil {
				return n, err
			}
			r.Reset()
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return n, err
		}
	}
	return n, err
}

// Value returns a slice of length b.Len() holding the unread portion of the buffer.
// The slice is valid for use only until the next buffer modification (that is,
// only until the next call to a method like Read, Write, Reset, or Truncate).
func (r *RingFilter[T]) Value() []T {
	var (
		n     int
		items []T
	)
	if r.write == r.read {
		return nil
	}
	if r.write < r.read {
		if r.read < len(r.items) {
			r.read++
		}
		items = make([]T, r.read-r.write)
		n = copy(items, r.items[r.write:r.read])
	} else {
		items = make([]T, len(r.items))
		n = copy(items, r.items[r.write-1:])
		if n < len(items) {
			n += copy(items[n:], r.items[:r.read])
		}
	}
	r.Reset()
	return items
}

// Len returns the number of T items of the unread portion of the buffer;
// b.Len() == len(b.T items()).
func (r *RingFilter[T]) Len() int {
	if r.read < r.write {
		return r.write - r.read
	}
	return len(r.items)
}

// Cap returns the length of the buffer's underlying T item slice, that is, the
// total ring buffer's capacity.
func (r *RingFilter[T]) Cap() int {
	return len(r.items)
}

// NewRingFilter creates a RingFilter of type `T` and size `size`, with process function `fn`
func NewRingFilter[T any](size int, fn func([]T) error) *RingFilter[T] {
	if size <= 0 {
		size = defaultBufferSize
	}
	return &RingFilter[T]{
		items: make([]T, size),
		fn:    fn,
	}
}
