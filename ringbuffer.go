package gbuf

import "io"

// RingBuffer is a buffer that is connected end-to-end, which allows continuous
// reads and writes provided that the caller is aware of potential loss of read data
// (as elements are overwritten if not read)
type RingBuffer[T any] struct {
	items []T
	start int
	end   int
}

// Write sets the contents of `p` to the buffer, in sequential order,
// looping through the buffer if needed. The return value n is the
// length of p; err is always nil. If the index in the buffer has not
// been yet read, it will be overwritten
func (r *RingBuffer[T]) Write(p []T) (n int, err error) {
	for i := range p {
		r.items[r.end] = p[i]
		r.end = (r.end + 1) % len(r.items)
		if r.end == r.start {
			r.start = (r.start + 1) % len(r.items)
		}
	}
	return len(p), nil
}

// WriteItem writes the T `item` to the buffer in the next position
// The returned error is always nil, but is included to match gio.Writer's
// WriteItem. If the index in the buffer has not been yet read, it will be
// overwritten
func (r *RingBuffer[T]) WriteItem(item T) (err error) {
	r.items[r.end] = item
	r.end = (r.end + 1) % len(r.items)
	if r.end == r.start {
		r.start = (r.start + 1) % len(r.items)
	}
	return nil
}

// Read reads the next len(p) T items from the buffer or until the buffer
// is drained. The return value n is the number of T items read. If the
// buffer has no data to return, err is io.EOF (unless len(p) is zero);
// otherwise it is nil.
func (r *RingBuffer[T]) Read(p []T) (n int, err error) {
	if r.start == r.end {
		return 0, io.EOF
	}
	if r.start < r.end {
		n = copy(p, r.items[r.start:r.end])
	} else {
		n = copy(p, r.items[r.start:])
		if n < len(p) {
			n += copy(p[n:], r.items[:r.end])
		}
	}
	r.start = (r.start + n) % len(r.items)
	return n, nil
}

// ReadItem reads and returns the next T item from the buffer.
// If no T item is available, it returns error io.EOF.
func (r *RingBuffer[T]) ReadItem() (item T, err error) {
	item = r.items[r.start]
	r.start = (r.start + 1) % len(r.items)
	return item, nil
}

// NewRingBuffer creates a RingBuffer of type `T` and size `size`
func NewRingBuffer[T any](size int) *RingBuffer[T] {
	return &RingBuffer[T]{
		items: make([]T, size),
	}
}
