package gbufio

// ReadWriter stores pointers to a Reader and a Writer.
// It implements gio.ReadWriter.
type ReadWriter[T any] struct {
	*Reader[T]
	*Writer[T]
}

// NewReadWriter allocates a new ReadWriter that dispatches to r and w.
func NewReadWriter[T any](r *Reader[T], w *Writer[T]) *ReadWriter[T] {
	return &ReadWriter[T]{r, w}
}
