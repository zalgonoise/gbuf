package gbuf

import "testing"

func TestRingFilterWrite(t *testing.T) {
	t.Run("Simple", func(t *testing.T) {
		input := []byte("very long string buffered every 5 characters")
		var output = make([]byte, 0, len(input))
		r := NewRingFilter(4, func(b []byte) {
			output = append(output, b...)
			t.Log(string(b), "--", string(output))
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
