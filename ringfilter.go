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
	// indicates whether the write operation is currently overwriting the buffer, so that r.read follows r.write
	full bool

	write int
	read  int

	fn    func([]T) error
	items []T
}

func (r *RingFilter[T]) writeWithinBounds(p []T, end int) (n int, err error) {
	n = copy(r.items[r.write:end], p)

	err = r.fn(p)

	if r.write < r.read && end >= r.read {
		r.full = true
	}

	r.write = end

	if r.full {
		r.read = end
	}

	if err != nil {
		return n, err
	}

	return n, nil
}

func (r *RingFilter[T]) writeWrapped(p []T, end int) (n int, err error) {
	endWrapped := end % len(r.items)
	n = copy(r.items[r.write:len(r.items)], p)
	n += copy(r.items[:endWrapped], p[n:])

	err = r.fn(p)

	if r.write < r.read && end >= r.read {
		r.full = true
	}

	r.write = endWrapped

	if r.full || endWrapped >= r.read {
		// overwritten items, set read point to write
		r.read = r.write
		r.full = true
	}

	if err != nil {
		return n, err
	}

	return n, nil
}

func (r *RingFilter[T]) writeWithinCapacity(p []T) (n int, err error) {
	var end = r.write + len(p)

	if end < len(r.items) {
		return r.writeWithinBounds(p, end)
	}

	return r.writeWrapped(p, end)
}

// Write sets the contents of `p` to the buffer, in sequential order.
// The return value n is the length of p; err comes from the process func.
// If the index in the buffer has not been yet read, the entire unread
// buffer value is sent to the configured process function
func (r *RingFilter[T]) Write(p []T) (n int, err error) {
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
	r.full = true

	err = r.fn(p)
	if err != nil {
		return n, err
	}

	return ringLn, nil
}

// WriteItem writes the T `item` to the buffer in the next position
// The returned error is always nil, but is included to match gio.Writer's
// WriteItem. If the index in the buffer has not been yet read, it will be
// overwritten
func (r *RingFilter[T]) WriteItem(item T) (err error) {
	pos := (r.write + 1) % len(r.items)

	if r.full {
		r.read = pos
	} else if pos == r.read {
		r.full = true
	}

	r.items[r.write] = item
	r.write = pos

	return r.fn([]T{item})
}

// WriteTo writes data to w until the buffer is drained or an error occurs.
// The return value n is the number of T items written; it always fits into an
// int, but it is int64 to match the gio.WriterTo interface. Any error
// encountered during the write operation is also returned.
func (r *RingFilter[T]) WriteTo(b gio.Writer[T]) (n int64, err error) {
	var num int

	switch {
	case r.read >= r.write:
		num, err = b.Write(r.items[r.read:len(r.items)])

		if num < 0 {
			return n, ErrRingFilterNegativeRead
		}

		n += int64(num)
		r.full = false
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

		if num < 0 {
			return n, ErrRingFilterNegativeRead
		}

		n += int64(num)
		r.full = false
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

// Read reads the next len(p) T items from the buffer or until the buffer
// is drained. The return value n is the number of T items read. If the
// buffer has no data to return, err is io.EOF (unless len(p) is zero);
// otherwise it is nil.
func (r *RingFilter[T]) Read(p []T) (n int, err error) {
	itemLen := len(p)

	if itemLen == 0 || r.Len() == 0 {
		return 0, nil
	}

	switch {
	case r.read >= r.write:
		n += copy(p, r.items[r.read:])
		r.full = false

		// don't keep writing if there isn't enough space in p
		if n >= itemLen {
			r.read = (r.read + n) % len(r.items)

			return n, nil
		}

		n += copy(p[n:], r.items[:r.write])
		r.read = (r.read + n) % len(r.items)

		return n, nil
	default:
		n = copy(p, r.items[r.read:r.write])
		r.read += n

		return n, nil
	}
}

// ReadFrom reads data from b until EOF and appends it to the buffer, cycling
// the buffer as needed. For each complete cycle, the process function is called
// with the full buffer in the ring, and the ring reset.
// The return value n is the number of T items read. Any error except io.EOF
// encountered during the read is also returned.
func (r *RingFilter[T]) ReadFrom(b gio.Reader[T]) (n int64, err error) {
	var (
		// initialize a counter for each iteration's written items
		num int
	)

	// read the stream's items until the reader is depleted of any more data
	for {
		// read from r.write until the end of the RingFilter buffer
		num, err = b.Read(r.items[r.write:])

		if num < 0 {
			return n, ErrRingFilterNegativeRead
		}

		if r.write < r.read && r.write+num >= r.read {
			r.full = true
		}

		// early exit if io.EOF is raised
		if errors.Is(err, io.EOF) {
			// io.EOF indicates the input gio.Reader is depleted, exit without raising an error as the operation was OK
			return n, nil
		}

		if err != nil {
			return n, err
		}

		if len(r.items)-(r.write-r.read) == num {
			// if the number of written items fills the buffer, the read index must follow the write index
			r.full = true
			r.read = r.write
		}

		n += int64(num)

		if errFilter := r.fn(r.items[r.write : r.write+num]); errFilter != nil {
			return n, errFilter
		}

		r.write = (r.write + num) % len(r.items)

		if r.full {
			r.read = r.write
		}

	}
}

// Value returns a slice of length b.Len() holding the unread portion of the buffer.
// The slice is valid for use only until the next buffer modification (that is,
// only until the next call to a method like Read, Write, Reset, or Truncate).
func (r *RingFilter[T]) Value() (items []T) {
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

// Len returns the number of T items of the unread portion of the buffer;
// b.Len() == len(b.T items()).
func (r *RingFilter[T]) Len() int {
	if r.full {
		return len(r.items)
	}

	return (len(r.items) + (r.write - r.read)) % len(r.items)
}

// Cap returns the length of the buffer's underlying T item slice, that is, the
// total ring buffer's capacity.
func (r *RingFilter[T]) Cap() int {
	return len(r.items)
}

// Truncate clears `n` items from the read index of the buffer onwards, setting it to a default zero
// value for the type T
func (r *RingFilter[T]) Truncate(n int) {
	switch {
	case n <= 0, n > r.Len():
		r.Reset()
	default:
		clear(r.items[r.read : r.read+n])
	}
}

// Reset resets the buffer to be empty,
// but it retains the underlying storage for use by future writes.
// Reset is the same as Truncate().
func (r *RingFilter[T]) Reset() {
	r.read = 0
	r.write = 0

	// reset the buffer preventing further reads to show the previous data
	clear(r.items)
}

func (r *RingFilter[T]) Next(n int) (items []T) {
	switch {
	case n <= 0:
		return nil
	case n > r.Len():
		return r.Value()
	default:
		items = make([]T, n)
		if _, err := r.Read(items); err != nil {
			return nil
		}

		return items
	}
}

func (r *RingFilter[T]) UnreadItem() error {
	if r.read == r.write {
		return ErrRingFilterUnreadItem
	}

	r.read = (r.read - 1) % len(r.items)

	return nil
}

func (r *RingFilter[T]) ReadItems(delim func(T) bool) (line []T, err error) {
	line = make([]T, 0, len(r.items))

scanLoop:
	switch {
	case r.read >= r.write:
		for i := r.read; i < len(r.items); i++ {
			if delim(r.items[i]) {
				break scanLoop
			}

			line = append(line, r.items[i])
		}

		fallthrough
	default:
		for i := 0; i < r.write; i++ {
			if delim(r.items[i]) {
				break scanLoop
			}

			line = append(line, r.items[i])
		}
	}

	if len(line) == 0 {
		return nil, nil
	}

	r.read = (r.read + len(line)) % len(r.items)
	r.full = false

	return line[:len(line):len(line)], nil
}

// ReadItem reads and returns the next T item from the buffer.
// If no T item is available, it returns error io.EOF.
func (r *RingFilter[T]) ReadItem() (item T, err error) {
	item = r.items[r.read]
	r.read = (r.read + 1) % len(r.items)
	r.full = false

	return item, nil
}

// Seek implements the gio.Seeker interface. All valid whence will point to
// the current cursor's (read) position
func (r *RingFilter[T]) Seek(offset int64, whence int) (abs int64, err error) {
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

// NewRingFilter creates a RingFilter of type `T` and size `size`, with process function `fn`
func NewRingFilter[T any](size int, fn func([]T) error) *RingFilter[T] {
	if size <= 0 {
		size = defaultBufferSize
	}

	if fn == nil {
		fn = unimplementedFilter[T]
	}

	return &RingFilter[T]{
		items: make([]T, size),
		fn:    fn,
	}
}

func unimplementedFilter[T any]([]T) error {
	return nil
}
