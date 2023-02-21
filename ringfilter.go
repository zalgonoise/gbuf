package gbuf

import "io"

// RingBuffer is a buffer that is connected end-to-end, which allows continuous
// reads and writes where the caller configures a callback function to process
// all items on each loop
type RingFilter[T any] struct {
	items []T
	start int
	end   int
	fn    func([]T)
}

// Write sets the contents of `p` to the buffer, in sequential order.
// The return value n is the length of p; err is always nil. If the
// index in the buffer has not been yet read, the entire unread buffer
// value is sent to the configured process function
func (r *RingFilter[T]) Write(p []T) (n int, err error) {
	for i := range p {
		r.items[r.end] = p[i]
		if (r.end+1)%len(r.items) == r.start {
			if r.end < len(r.items) {
				r.end++
			}
			r.fn(r.items[r.start:r.end])
			r.Reset()
			continue
		}
		r.end = (r.end + 1) % len(r.items)
	}
	return len(p), nil
}

// Reset resets the buffer to be empty,
// but it retains the underlying storage for use by future writes.
func (r *RingFilter[T]) Reset() {
	r.start = 0
	r.end = 0
}

// Read reads the next len(p) T items from the buffer or until the buffer
// is drained. The return value n is the number of T items read. If the
// buffer has no data to return, err is io.EOF (unless len(p) is zero);
// otherwise it is nil.
func (r *RingFilter[T]) Read(p []T) (n int, err error) {
	if r.start == r.end {
		return 0, io.EOF
	}
	if r.start < r.end {
		n = copy(p, r.items[r.start:r.end])
	} else {
		n = copy(p, r.items[r.start-1:])
		if n < len(p) {
			n += copy(p[n:], r.items[:r.end])
		}
	}
	r.start = (r.start + n) % len(r.items)
	return n, nil
}

// Value returns a slice of length b.Len() holding the unread portion of the buffer.
// The slice is valid for use only until the next buffer modification (that is,
// only until the next call to a method like Read, Write, Reset, or Truncate).
func (r *RingFilter[T]) Value() []T {
	var (
		n     int
		items []T
	)
	if r.start == r.end {
		return nil
	}
	if r.start < r.end {
		if r.end < len(r.items) {
			r.end++
		}
		items = make([]T, r.end-r.start)
		n = copy(items, r.items[r.start:r.end])
	} else {
		items = make([]T, len(r.items))
		n = copy(items, r.items[r.start-1:])
		if n < len(items) {
			n += copy(items[n:], r.items[:r.end])
		}
	}
	r.Reset()
	return items
}

// Len returns the number of T items of the unread portion of the buffer;
// b.Len() == len(b.T items()).
func (r *RingFilter[T]) Len() int {
	if r.start < r.end {
		return r.end - r.start
	}
	return len(r.items)
}

// Cap returns the length of the buffer's underlying T item slice, that is, the
// total ring buffer's capacity.
func (r *RingFilter[T]) Cap() int {
	return len(r.items)
}

// NewRingFilter creates a RingFilter of type `T` and size `size`, with process function `fn`
func NewRingFilter[T any](size int, fn func([]T)) *RingFilter[T] {
	if size <= 0 {
		size = defaultBufferSize
	}
	return &RingFilter[T]{
		items: make([]T, size),
		fn:    fn,
	}
}
