package gbuf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRingFilter_Write(t *testing.T) {
	for _, testcase := range []struct {
		name  string
		input string
		size  int
	}{
		{
			name:  "Simple",
			input: "very long string buffered every 5 characters",
			size:  5,
		},
		{
			name:  "Short",
			input: "x",
			size:  10,
		},
		{
			name:  "ByteAtATime",
			input: "one byte at a time",
			size:  1,
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			var output = make([]byte, 0, len(testcase.input))

			r := NewRingFilter(testcase.size, func(b []byte) error {
				output = append(output, b...)
				return nil
			})

			_, err := r.Write([]byte(testcase.input))
			require.NoError(t, err)
			require.Equal(t, string(output), testcase.input)
		})
	}
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
