package gbuf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
)

func TestRingFilterWrite(t *testing.T) {
	t.Run("Simple", func(t *testing.T) {
		input := []byte("very long string buffered every 5 characters")
		var output = make([]byte, 0, len(input))
		r := NewRingFilter(4, func(b []byte) error {
			output = append(output, b...)
			return nil
		})
		_, err := r.Write(input)
		if err != nil {
			t.Error(err)
		}
		if string(output) != string(input) {
			t.Errorf("output mismatch error: wanted %s ; got %s -- %v", string(input), string(output), r.items)
		}
	})

}

func TestRingFilterReadFrom(t *testing.T) {
	t.Run("Simple", func(t *testing.T) {
		inputString := []byte("very long string buffered every 5 characters")
		input := bytes.NewReader(inputString)
		var output = make([]byte, 0, len(inputString))
		r := NewRingFilter(4, func(b []byte) error {
			output = append(output, b...)
			return nil
		})
		_, err := r.ReadFrom(input)
		if err != nil {
			t.Error(err)
		}
		if string(output) != string(inputString) {
			t.Errorf("output mismatch error: wanted %s ; got %s -- %v", string(inputString), string(output), r.items)
		}
	})

	t.Run("Complex", func(t *testing.T) {
		inputString := []byte("very long string buffered every 5 characters")
		wants := "long string buffered every 5 characters"
		input := bytes.NewReader(inputString)
		out := make([]byte, len(wants)-3) // -3 is for the bytes flushed from the ring
		r := NewRingFilter(4, func(b []byte) error {
			for i := range b {
				if b[i] == ' ' {
					fmt.Println(string(b[i:]))
					_, err := input.Read(out)
					if err != nil && !errors.Is(err, io.EOF) {
						return err
					}
					out = append(b[i+1:], out...)
					return nil
				}
			}
			return nil
		})
		_, err := r.ReadFrom(input)
		if err != nil {
			t.Error(err)
		}
		if wants != string(out) {
			t.Errorf("output mismatch error: wanted %s ; got %s -- %v", wants, string(out), r.items)
		}
	})

}
