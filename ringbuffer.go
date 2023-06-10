package gbuf

import (
	"errors"
	"io"

	"github.com/zalgonoise/gio"
)

const defaultBufferSize = 256

var (
	ErrRingNegativePosition = errors.New("gbuf.RingBuffer.Seek: negative position")
	ErrRingInvalidWhence    = errors.New("gbuf.RingBuffer.Seek: invalid whence")
)

// RingBuffer is a buffer that is connected end-to-end, which allows continuous
// reads and writes provided that the caller is aware of potential loss of read data
// (as elements are overwritten if not read)
type RingBuffer[T any] struct {
	write int
	read  int
	items []T
}

func (r *RingBuffer[T]) writeWithinBounds(p []T, end int) (n int, err error) {
	n = copy(r.items[r.write:end], p)
	r.write = end

	return n, nil
}

func (r *RingBuffer[T]) writeWrapped(p []T, end int) (n int, err error) {
	end %= len(r.items)
	n = copy(r.items[r.write:len(r.items)], p)
	n += copy(r.items[:end], p[n:])
	r.write = end

	if end > r.read {
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

	return ringLn, nil
}

// WriteItem writes the T `item` to the buffer in the next position
// The returned error is always nil, but is included to match gio.Writer's
// WriteItem. If the index in the buffer has not been yet read, it will be
// overwritten
func (r *RingBuffer[T]) WriteItem(item T) (err error) {
	pos := (r.write + 1) % len(r.items)
	if r.write == r.read {
		r.read = pos
	}

	r.write = pos
	r.items[r.write] = item

	return nil
}

// Read reads the next len(p) T items from the buffer or until the buffer
// is drained. The return value n is the number of T items read. If the
// buffer has no data to return, err is io.EOF (unless len(p) is zero);
// otherwise it is nil.
func (r *RingBuffer[T]) Read(p []T) (n int, err error) {
	var (
		ln     = len(p)
		ringLn = r.Len()
	)

	switch {
	case r.read >= r.write:
		size := len(r.items) - r.read

		n += copy(p[:size], r.items[r.read:len(r.items)])
		r.read = 0

		// don't keep writing if there isn't enough space in p
		if n >= ln {
			return n, nil
		}

		n += copy(p[n:n+r.write], r.items[:r.write])
		r.read = r.write % len(r.items)

		return n, nil
	default:
		m := copy(p[:ringLn], r.items[r.read:r.write])

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
func (r *RingBuffer[T]) Value() []T {
	switch {
	case r.read < r.write:
		items := make([]T, r.write-r.read)
		copy(items, r.items[r.read:r.write])
		r.read = r.write

		return items
	default:
		items := make([]T, len(r.items))
		copy(items[:r.read], r.items[r.read:])
		copy(items[r.read:], r.items[:r.read])

		return items
	}
}

// Len returns the number of T items of the unread portion of the buffer;
// b.Len() == len(b.T items()).
func (r *RingBuffer[T]) Len() int {
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

// Truncate serves as an alias to Reset(); to preserve the ring buffer size
func (r *RingBuffer[T]) Truncate(int) {
	r.Reset()
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
	var num int

	for {
		num, err = b.Read(r.items[r.write:len(r.items)])

		if n < 0 {
			panic(errNegativeRead)
		}

		n += int64(num)

		r.write = (r.write + num) % len(r.items)

		if errors.Is(err, io.EOF) {
			return n, nil
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
func (r *RingBuffer[T]) ReadFromIf(b gio.Reader[T], cond func() bool) (n int64, err error) {
	var num int

	for cond() {
		num, err = b.Read(r.items[r.write:len(r.items)])

		if n < 0 {
			panic(errNegativeRead)
		}

		n += int64(num)

		r.write = (r.write + num) % len(r.items)

		if errors.Is(err, io.EOF) {
			return n, nil
		}

		if err != nil {
			return n, err
		}
	}

	return n, err
}

// WriteTo writes data to w until the buffer is drained or an error occurs.
// The return value n is the number of T items written; it always fits into an
// int, but it is int64 to match the gio.WriterTo interface. Any error
// encountered during the write is also returned.
func (r *RingBuffer[T]) WriteTo(b gio.Writer[T]) (n int64, err error) {
	var num int

	switch {
	case r.read >= r.write:
		num, err = b.Write(r.items[r.read:len(r.items)])

		if n < 0 {
			panic(errNegativeRead)
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
			panic(errNegativeRead)
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
	if n == 0 {
		return nil
	}

	// TODO: refactor
	//
	//	if n < 0 || n > r.Len() {
	//		panic("gbuf.RingBuffer: out of range")
	//	}
	//
	//	if r.start+n < r.end {
	//		items = append(items, r.items[r.start:r.start+n]...)
	//		r.start += n
	//
	//		return items
	//	}
	//
	//	items = append(items, r.items[r.start-1:]...)
	//	items = append(items, r.items[:n]...)
	//	r.start = n

	return items
}

func (r *RingBuffer[T]) UnreadItem() error {
	// TODO: refactor
	//	if r.start == r.end {
	//		return ErrUnreadItem
	//	}
	//
	//	r.start = (r.start - 1) % len(r.items)
	//
	return nil
}

func (r *RingBuffer[T]) ReadItems(delim func(T) bool) (line []T, err error) {
	_ = delim

	if r.read == r.write {
		return line, io.EOF
	}

	// TODO: refactor
	//
	//	if r.start < r.end {
	//		var i int
	//
	//		for i = r.start; i < r.end; i++ {
	//			if delim(r.items[i]) {
	//				break
	//			}
	//		}
	//
	//		line = append(line, r.items[r.start:r.start+i+1]...)
	//		r.start += i + 1
	//
	//		return line, nil
	//	}
	//
	//	var (
	//		i    int
	//		done bool
	//	)
	//
	//	for i = r.start; i < len(r.items); i++ {
	//		if delim(r.items[i]) {
	//			done = true
	//			break
	//		}
	//	}
	//
	//	line = append(line, r.items[r.start:r.start+i+1]...)
	//
	//	if !done {
	//		for i = 0; i < r.end; i++ {
	//			if delim(r.items[i]) {
	//				break
	//			}
	//		}
	//
	//		line = append(line, r.items[r.start:r.start+i+1]...)
	//	}

	return line, nil
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
	_, _ = offset, whence

	// TODO: refactor
	//
	//	var abs int64
	//
	//	switch whence {
	//	case gio.SeekStart, gio.SeekCurrent, gio.SeekEnd:
	//		if (r.start + int(offset)) < len(r.items) {
	//			abs = int64(r.start) + offset
	//		} else {
	//			abs = offset - int64(len(r.items)-r.start)
	//		}
	//	default:
	//		return 0, ErrRingInvalidWhence
	//	}
	//
	//	if abs < 0 {
	//		return 0, ErrRingNegativePosition
	//	}
	//
	//	r.start = int(abs)

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
