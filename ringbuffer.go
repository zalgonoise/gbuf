package gbuf

import (
	"errors"
	"io"

	"github.com/zalgonoise/gio"
)

const defaultBufferSize = 256 // minimum value when unset

// RingBuffer is a buffer that is connected end-to-end, which allows continuous
// reads and writes provided that the caller is aware of potential loss of read data
// (as elements are overwritten if not read)
type RingBuffer[T any] struct {
	// indicates whether the write operation is currently overwriting the buffer, so that r.read follows r.write
	isFull bool

	write int
	read  int

	items []T
}

func (r *RingBuffer[T]) writeWithinBounds(p []T, end int) (n int, err error) {
	n = copy(r.items[r.write:end], p)
	r.write = end

	if r.isFull {
		r.read = end
	}

	return n, nil
}

func (r *RingBuffer[T]) writeWrapped(p []T, end int) (n int, err error) {
	end %= len(r.items)
	n = copy(r.items[r.write:len(r.items)], p)
	n += copy(r.items[:end], p[n:])

	r.write = end

	if r.isFull || end > r.read {
		// overwritten items, set read point to write
		r.read = r.write
	}

	return n, nil
}

func (r *RingBuffer[T]) writeWithinCapacity(p []T) (n int, err error) {
	var end = r.write + len(p)

	if end <= len(r.items) {
		return r.writeWithinBounds(p, end)
	}

	return r.writeWrapped(p, end)
}

// Write sets the contents of `p` to the buffer, in sequential order,
// looping through the buffer if needed. The return value n is the
// length of p; err is always nil. If the index in the buffer has not
// been yet read, it will be overwritten
func (r *RingBuffer[T]) Write(p []T) (n int, err error) {
	var (
		ln     = len(p)
		ringLn = len(r.items)
	)

	if ln < ringLn {
		return r.writeWithinCapacity(p)
	}

	// no need to copy all the items if they don't fit
	// copy the last portion of the input of the same size as the buffer, and reset the
	// read index to be on the write index. The filter func will consume the entire input buffer, however
	copy(r.items, p[ln-ringLn:])
	// full circle, reset read index to write point
	r.read = r.write
	r.isFull = true

	return ringLn, nil
}

// WriteItem writes the T `item` to the buffer in the next position
// The returned error is always nil, but is included to match gio.Writer's
// WriteItem. If the index in the buffer has not been yet read, it will be
// overwritten
func (r *RingBuffer[T]) WriteItem(item T) (err error) {
	pos := (r.write + 1) % len(r.items)

	if r.isFull {
		r.read = pos
	} else if pos == r.read {
		r.isFull = true
	}

	r.items[r.write] = item
	r.write = pos

	return nil
}

// Read reads the next len(p) T items from the buffer or until the buffer
// is drained. The return value n is the number of T items read. If the
// buffer has no data to return, err is io.EOF (unless len(p) is zero);
// otherwise it is nil.
func (r *RingBuffer[T]) Read(p []T) (n int, err error) {
	ln := len(p)

	switch {
	case r.read >= r.write:
		n += copy(p, r.items[r.read:])
		r.read = (r.read + n) % len(r.items)

		// don't keep writing if there isn't enough space in p
		if n >= ln {
			return n, nil
		}

		n += copy(p[n:n+r.write], r.items[:r.write])
		r.read = r.write % len(r.items)

		return n, nil
	default:
		m := copy(p, r.items[r.read:r.write])

		if ln < m {
			n = ln
		} else {
			n = m
		}

		r.read += n

		return n, nil
	}
}

// Value returns a slice of length b.Len() holding the unread portion of the buffer.
// The slice is valid for use only until the next buffer modification (that is,
// only until the next call to a method like Read, Write, Reset, or Truncate).
func (r *RingBuffer[T]) Value() (items []T) {
	switch {
	case r.read < r.write:
		items = make([]T, r.write-r.read)
		copy(items, r.items[r.read:r.write])
		r.read = r.write

	case r.read == 0:
		items = make([]T, len(r.items))
		copy(items, r.items)
	default:
		items = make([]T, len(r.items))
		copy(items[:len(r.items)-r.read], r.items[r.read:])
		copy(items[len(r.items)-r.read:], r.items[:r.read])
	}

	return items
}

// Len returns the number of T items of the unread portion of the buffer.
func (r *RingBuffer[T]) Len() int {
	if r.read == r.write && !r.isFull {
		return 0
	}

	if r.read < r.write {
		return r.write - r.read
	}

	return len(r.items) - (r.read - r.write)
}

// Cap returns the length of the buffer's underlying T item slice, that is, the
// total ring buffer's capacity.
func (r *RingBuffer[T]) Cap() int {
	return len(r.items)
}

// Truncate clears `n` items from the read index of the buffer onwards, setting it to a default zero
// value for the type T
func (r *RingBuffer[T]) Truncate(n int) {
	switch {
	case n <= 0, n > r.Len():
		r.Reset()
	default:
		for i, j := r.read, n; j > 0; i, j = (i+1)%len(r.items), j-1 {
			var zero T

			r.items[i] = zero
			r.read = (r.read + 1) % len(r.items)
		}
	}
}

// Reset resets the buffer to be empty,
// but it retains the underlying storage for use by future writes.
// Reset is the same as Truncate().
func (r *RingBuffer[T]) Reset() {
	r.read = 0
	r.write = 0

	// reset the buffer preventing further reads to show the previous data
	for i := range r.items {
		var zero T

		r.items[i] = zero
	}
}

// ReadFrom reads data from b until EOF and appends it to the buffer, cycling
// the buffer as needed. Any unready bytes will be overwritten on each cycle.
// The return value n is the number of T items read. Any error except io.EOF
// encountered during the read is also returned.
func (r *RingBuffer[T]) ReadFrom(b gio.Reader[T]) (n int64, err error) {
	var (
		// initialize a counter for each iteration's written items
		num int
	)

	// read the stream's items until the reader is depleted of any more data
	for {
		// read from r.write until the end of the RingFilter buffer
		num, err = b.Read(r.items[r.write:len(r.items)])
		if r.isFull {
			r.read = r.write
		}

		// early exit if io.EOF is raised
		if errors.Is(err, io.EOF) {
			// io.EOF indicates the input gio.Reader is depleted, exit without raising an error as the operation was OK
			return n, nil
		}

		if err != nil {
			return n, err
		}

		switch {
		case num < 0:
			return n, ErrRingFilterNegativeRead
		case num == len(r.items)-r.write:
			// if the number of written items fills the buffer, the read index must follow the write index
			r.isFull = true
			r.read = r.write
		}

		n += int64(num)

		r.write = (r.write + num) % len(r.items)
	}
}

// WriteTo writes data to w until the buffer is drained or an error occurs.
// The return value n is the number of T items written; it always fits into an
// int, but it is int64 to match the gio.WriterTo interface. Any error
// encountered during the write operation is also returned.
func (r *RingBuffer[T]) WriteTo(b gio.Writer[T]) (n int64, err error) {
	var num int

	switch {
	case r.read >= r.write:
		num, err = b.Write(r.items[r.read:len(r.items)])

		if n < 0 {
			return n, ErrRingBufferNegativeRead
		}

		n += int64(num)

		r.read = (r.read + num) % len(r.items)

		if errors.Is(err, io.EOF) {
			return n, nil
		}

		if err != nil {
			return n, err
		}

		fallthrough // write from [0:r.write]
	default:
		num, err = b.Write(r.items[r.read:r.write])

		if n < 0 {
			return n, ErrRingBufferNegativeRead
		}

		n += int64(num)

		r.read = (r.read + num) % len(r.items)

		if errors.Is(err, io.EOF) {
			return n, nil
		}

		if err != nil {
			return n, err
		}
	}

	return n, nil
}

func (r *RingBuffer[T]) Next(n int) (items []T) {
	switch {
	case n <= 0:
		return nil
	case n > r.Len():
		return r.Value()
	default:
		items = make([]T, n)
		_, _ = r.Read(items)

		return items
	}
}

func (r *RingBuffer[T]) UnreadItem() error {
	if r.read == r.write {
		return ErrRingBufferUnreadItem
	}

	r.read = (r.read - 1) % len(r.items)

	return nil
}

func (r *RingBuffer[T]) ReadItems(delim func(T) bool) (line []T, err error) {
	line = make([]T, 0, len(r.items))

	switch {
	case r.read >= r.write:
		for i := r.read; i < len(r.items); i++ {
			if delim(r.items[i]) {
				return line[:len(line):len(line)], nil
			}

			line = append(line, r.items[i])
		}

		fallthrough
	default:
		for i := 0; i < r.write; i++ {
			if delim(r.items[i]) {
				return line[:len(line):len(line)], nil
			}

			line = append(line, r.items[i])
		}
	}

	return line[:len(line):len(line)], nil
}

// ReadItem reads and returns the next T item from the buffer.
// If no T item is available, it returns error io.EOF.
func (r *RingBuffer[T]) ReadItem() (item T, err error) {
	item = r.items[r.read]
	r.read = (r.read + 1) % len(r.items)

	return item, nil
}

// Seek implements the gio.Seeker interface. All valid whence will point to
// the current cursor's (read) position
func (r *RingBuffer[T]) Seek(offset int64, whence int) (abs int64, err error) {
	switch whence {
	case io.SeekEnd:
		abs = (int64(r.write) + offset) % int64(len(r.items))
	case io.SeekCurrent, io.SeekStart:
		abs = (int64(r.read) + offset) % int64(len(r.items))
	default:
		return 0, ErrRingFilterInvalidWhence
	}

	r.read = int(abs)

	return abs, nil
}

// NewRingBuffer creates a RingBuffer of type `T` and size `size`
func NewRingBuffer[T any](size int) *RingBuffer[T] {
	if size <= 0 {
		size = defaultBufferSize
	}

	return &RingBuffer[T]{
		items: make([]T, size),
	}
}
