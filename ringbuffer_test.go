package gbuf

import (
	"errors"
	"io"
	"testing"
)

func TestRingBufferWrite(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		input := []byte("hello world")
		r := NewRingBuffer[byte](11)
		n, err := r.Write(input)
		if err != nil {
			t.Error(err)
		}
		if n != len(input) {
			t.Errorf("unexpected number of bytes written: wanted %d ; got %d", len(input), n)
		}
		if string(r.items) != string(input) {
			t.Errorf("written data mismatch: wanted %s ; got %s", string(input), string(r.items))
		}
	})
}

func TestRingBufferWriteItem(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		input := []byte("hello world")
		r := NewRingBuffer[byte](11)
		for _, b := range input {
			err := r.WriteItem(b)
			if err != nil {
				t.Error(err)
			}
		}
		if string(r.items) != string(input) {
			t.Errorf("written data mismatch: wanted %s ; got %s", string(input), string(r.items))
		}
	})
}

func TestRingBufferRead(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		input := []byte("hello world")
		output := make([]byte, 11)
		r := NewRingBuffer[byte](11)
		_, err := r.Write(input)
		if err != nil {
			t.Error(err)
			return
		}

		n, err := r.Read(output)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Error(err)
		}
		if n != len(input) {
			t.Errorf("unexpected number of bytes written: wanted %d ; got %d", len(input), n)
		}
		if string(output) != string(input) {
			t.Errorf("written data mismatch: wanted %s ; got %s", string(input), string(output))
		}
	})

	t.Run("SuccessAroundRing", func(t *testing.T) {
		input := []byte("hello world")
		wants := []byte("llo world")
		output := make([]byte, 9)
		r := NewRingBuffer[byte](9)
		for idx, b := range input {
			err := r.WriteItem(b)
			if err != nil {
				t.Error(err)
			}
			// advance two positions on idx 5
			if idx == 5 {
				_, _ = r.ReadItem()
				_, _ = r.ReadItem()
			}
		}

		_, err := r.Write(input)
		if err != nil {
			t.Error(err)
			return
		}

		n, err := r.Read(output)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Error(err)
		}
		if n != len(wants) {
			t.Errorf("unexpected number of bytes written: wanted %d ; got %d", len(wants), n)
		}
		if string(output) != string(wants) {
			t.Errorf("written data mismatch: wanted %s ; got %s", string(wants), string(output))
		}
	})

	t.Run("Overflow", func(t *testing.T) {
		input := []byte("hello world")
		wants := []byte("ld")
		output := make([]byte, 2)
		r := NewRingBuffer[byte](3)
		_, err := r.Write(input)
		if err != nil {
			t.Error(err)
			return
		}

		n, err := r.Read(output)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Error(err)
		}
		if n != len(wants) {
			t.Errorf("unexpected number of bytes written: wanted %d ; got %d", len(wants), n)
		}
		if string(output) != string(wants) {
			t.Errorf("written data mismatch: wanted %s ; got %s", string(wants), string(output))
		}
	})

	t.Run("FailEOF", func(t *testing.T) {
		output := make([]byte, 3)
		r := NewRingBuffer[byte](3)

		_, err := r.Read(output)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Error(err)
		}
	})
}

func TestRingBufferValue(t *testing.T) {
	t.Run("FittingBuffer", func(t *testing.T) {
		input := []byte("hello world")
		wants := []byte("hello world")

		r := NewRingBuffer[byte](15)
		_, err := r.Write(input)
		if err != nil {
			t.Error(err)
		}
		v := r.Value()
		if string(v) != string(wants) {
			t.Errorf("output mismatch error: wanted %v ; got %v", wants, v)
		}
	})
	t.Run("BufferOverflow", func(t *testing.T) {
		input := []byte("hello world")
		wants := []byte("llo world")

		r := NewRingBuffer[byte](9)
		_, err := r.Write(input)
		if err != nil {
			t.Error(err)
		}
		v := r.Value()
		if string(v) != string(wants) {
			t.Errorf("output mismatch error: wanted %s ; got %s", string(wants), string(v))
		}
	})

	t.Run("FailEOF", func(t *testing.T) {
		r := NewRingBuffer[byte](3)
		v := r.Value()
		if v != nil {
			t.Errorf("unexpected output: %v ; wanted <nil>", v)
		}
	})
}
