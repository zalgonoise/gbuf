package gbuf

import (
	"bytes"
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
			require.Equal(t, testcase.input, string(output))
		})
	}
}

func TestRingFilter_Write_Sequential(t *testing.T) {
	for _, testcase := range []struct {
		name       string
		input      string
		size       int
		chunkSizes []int
	}{
		{
			name:       "Simple",
			input:      "very long string buffered every 5 characters",
			size:       5,
			chunkSizes: []int{5, 5, 5, 5, 5, 5, 5, 5, 4},
		},
		{
			name:       "Short",
			input:      "x",
			size:       10,
			chunkSizes: []int{1},
		},
		{
			name:       "ByteAtATime",
			input:      "one byte",
			size:       1,
			chunkSizes: []int{1, 1, 1, 1, 1, 1, 1, 1},
		},
		{
			name:       "InconsistentWrite",
			input:      "a very long string that is stuck on the streaming wheel, clearly",
			size:       5,
			chunkSizes: []int{3, 12, 5, 20, 10, 14},
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			var output = make([]byte, 0, len(testcase.input))
			var n int

			r := NewRingFilter(testcase.size, func(b []byte) error {
				output = append(output, b...)
				return nil
			})

			for _, size := range testcase.chunkSizes {
				_, err := r.Write([]byte(testcase.input)[n : n+size])
				require.NoError(t, err)
				n += size
			}
			require.Equal(t, testcase.input, string(output))
		})
	}
}

func TestRingFilter_Read(t *testing.T) {
	for _, testcase := range []struct {
		name     string
		input    string
		size     int
		readSize int
		wants    string
		err      error
	}{
		{
			name:     "Simple",
			input:    "very long string buffered every 5 characters",
			size:     5,
			readSize: 5,
			wants:    "cters",
		},
		{
			name:     "Short",
			input:    "x",
			size:     10,
			readSize: 10,
			wants:    "x\x00\x00\x00\x00\x00\x00\x00\x00\x00", // zero bytes as buffer isn't filled
		},
		{
			name:     "ByteAtATime",
			input:    "one byte at a time",
			size:     1,
			readSize: 1,
			wants:    "e",
		},
		{
			name:     "Full",
			input:    "complete string",
			size:     15,
			readSize: 15,
			wants:    "complete string",
		},
		{
			name:     "FullWithExtraSpace",
			input:    "complete string",
			size:     20,
			readSize: 20,
			wants:    "complete string\x00\x00\x00\x00\x00",
		},
		{
			name:     "FullWithShortRead",
			input:    "string",
			size:     6,
			readSize: 3,
			wants:    "str",
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			r := NewRingFilter(testcase.size, func(b []byte) error {
				return nil
			})

			_, err := r.Write([]byte(testcase.input))
			require.NoError(t, err)

			buf := make([]byte, testcase.readSize)
			_, err = r.Read(buf)
			require.ErrorIs(t, err, testcase.err)
			require.Equal(t, testcase.wants, string(buf))
		})
	}
}

func TestRingFilter_WriteRead_Interleaved(t *testing.T) {
	type writeRead struct {
		write, read int
	}

	for _, testcase := range []struct {
		name        string
		input       string
		wantsFilter string
		wantsRead   string
		size        int
		chunkSizes  []writeRead // [write, read] pairs of operations
	}{
		{
			name:  "WriteWithSomeReads",
			input: "very long string buffered every 5 characters",
			size:  10,
			chunkSizes: []writeRead{
				{5, 0}, {3, 0}, {0, 4}, {7, 0},
			},
			wantsFilter: "very long strin",
			wantsRead:   "very",
		},
		{
			name:  "WritesSweepThroughReads",
			input: "very long string buffered every 5 characters",
			size:  10,
			chunkSizes: []writeRead{
				{8, 3}, {8, 4}, {8, 0},
			},
			wantsFilter: "very long string buffere",
			wantsRead:   "verong ",
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			var outputFilter = make([]byte, 0, len(testcase.input))
			var outputRead = make([]byte, len(testcase.input))
			var writeN int
			var readN int

			r := NewRingFilter(testcase.size, func(b []byte) error {
				outputFilter = append(outputFilter, b...)
				return nil
			})

			for _, wr := range testcase.chunkSizes {
				if wr.write > 0 {
					_, err := r.Write([]byte(testcase.input)[writeN : writeN+wr.write])
					require.NoError(t, err)
					writeN += wr.write
				}

				if wr.read > 0 {
					_, err := r.Read(outputRead[readN : readN+wr.read])
					require.NoError(t, err)
					readN += wr.read
				}
			}

			outputRead = outputRead[:readN:readN]

			require.Equal(t, testcase.wantsFilter, string(outputFilter))
			require.Equal(t, testcase.wantsRead, string(outputRead))
		})
	}
}

func BenchmarkRingFilter_Write(b *testing.B) {
	var (
		err   error
		input = []byte("this is a test string used to write into the buffer")
	)

	for _, testcase := range []struct {
		name string
		size int
	}{
		{
			name: "ShortSizeBuffer",
			size: 3,
		},
		{
			name: "MediumSizeBuffer",
			size: 10,
		},
		{
			name: "LargeSizeBuffer",
			size: 25,
		},
		{
			name: "FullSizeBuffer",
			size: 51,
		},
	} {
		b.Run(testcase.name, func(b *testing.B) {
			r := NewRingFilter(testcase.size, func(b []byte) error {
				return nil
			})

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err = r.Write(input)
				if err != nil {
					b.Error(err)
					return
				}
			}
		})
	}
}

func TestRingFilter_ReadFrom_Read(t *testing.T) {
	for _, testcase := range []struct {
		name  string
		input string
		wants string
		size  int
	}{
		{
			name:  "Simple/ProportionalBufferSize",
			input: "very long string buffered every 5 characters", // len: 44; 44 % 4 = 0
			wants: "ters",
			size:  4,
		},
		{
			name:  "Simple/OffsetBufferSize",
			input: "very long string buffered every 5 characters", // len: 44; 44 % 5 = 4
			wants: "cters",
			size:  5,
		},
		{
			name:  "Short",
			input: "x",
			wants: "x\x00\x00\x00\x00\x00\x00\x00\x00\x00",
			size:  10,
		},
		{
			name:  "ByteAtATime",
			input: "one byte at a time",
			wants: "e",
			size:  1,
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			var output = make([]byte, 0, len(testcase.input))

			r := NewRingFilter(testcase.size, func(b []byte) error {
				output = append(output, b...)
				return nil
			})

			_, err := r.ReadFrom(bytes.NewReader([]byte(testcase.input)))
			require.NoError(t, err)
			require.Equal(t, testcase.input, string(output))

			var read = make([]byte, testcase.size)
			n, err := r.Read(read)
			require.Greater(t, n, 0)
			require.NoError(t, err)
			require.Equal(t, testcase.wants, string(read))
		})
	}
}

func TestRingFilter_ReadFrom_Interleaved(t *testing.T) {
	for _, testcase := range []struct {
		name  string
		input []string
		wants string
		size  int
	}{
		{
			name:  "Simple/FollowReadPosition",
			input: []string{"very long stri", "ng ", "buffered ev", "ery", " 5 characters"}, // len: 44; 44 % 4 = 0
			wants: "ters",
			size:  4,
		},
		{
			name:  "Simple/WithinSize",
			input: []string{"very long ", "string buf", "fered ever", "y 10 chara", "cters"}, // len: 44; 44 % 5 = 4
			wants: "very long string buffered every 10 characters",
			size:  45,
		},
		{
			name:  "Short/WriteByteAtATime",
			input: []string{"x", "y", "z", "0", "1", "2", "3"},
			wants: "xyz0123\x00\x00\x00",
			size:  10,
		},
		{
			name:  "Short/SizeOne",
			input: []string{"keep a single byte", "no extras", "only store the last", "\x00", "character", "e"},
			wants: "e",
			size:  1,
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			var output = bytes.NewBuffer(make([]byte, 0, 128))

			r := NewRingFilter(testcase.size, func(b []byte) error {
				_, err := output.Write(b)
				return err
			})

			var swap bool

			for i := range testcase.input {

				if swap {
					_, err := r.Write([]byte(testcase.input[i]))
					require.NoError(t, err)
					require.Equal(t, testcase.input[i], output.String())
					output.Reset()

					swap = !swap
					continue
				}

				_, err := r.ReadFrom(bytes.NewReader([]byte(testcase.input[i])))
				require.NoError(t, err)
				require.Equal(t, testcase.input[i], output.String())
				output.Reset()

				swap = !swap
			}

			var read = make([]byte, testcase.size)
			n, err := r.Read(read)
			require.Greater(t, n, 0)
			require.NoError(t, err)
			require.Equal(t, testcase.wants, string(read))
		})
	}
}

func TestRingFilter_WriteTo(t *testing.T) {
	for _, testcase := range []struct {
		name  string
		input string
		size  int
		wants string
		err   error
	}{
		{
			name:  "Simple",
			input: "very long string buffered every 5 characters",
			size:  5,
			wants: "cters",
		},
		{
			name:  "Short",
			input: "x",
			size:  10,
			wants: "x",
		},
		{
			name:  "ByteAtATime",
			input: "one byte at a time",
			size:  1,
			wants: "e",
		},
		{
			name:  "Full",
			input: "complete string",
			size:  15,
			wants: "complete string",
		},
		{
			name:  "FullWithExtraSpace",
			input: "complete string",
			size:  20,
			wants: "complete string",
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			r := NewRingFilter(testcase.size, func(b []byte) error {
				return nil
			})

			_, err := r.Write([]byte(testcase.input))
			require.NoError(t, err)

			buf := bytes.NewBuffer(make([]byte, 0, testcase.size))
			_, err = r.WriteTo(buf)
			require.ErrorIs(t, err, testcase.err)
			require.Equal(t, testcase.wants, buf.String())
		})
	}
}

func TestRingFilter_ReadFrom_Nested_AsConverter(t *testing.T) {
	type nested struct {
		ints   *RingFilter[int8]
		floats *RingFilter[float64]
	}

	const maxInt8 float64 = 1<<7 - 1

	for _, testcase := range []struct {
		name  string
		input []int8
		wants []float64
		size  int
		newFn func() nested
	}{
		{
			name:  "Simple/3Items/WriteWithinBounds",
			input: []int8{1, 2, -3, -4, 5, 6, -7, -8, 9, 0, 127, -127},
			wants: []float64{0, 1, -1},
			size:  3,
			newFn: func() nested {
				n := nested{
					floats: NewRingFilter[float64](3, nil),
				}

				n.ints = NewRingFilter(3, func(data []int8) error {
					floats := make([]float64, len(data))
					for i := range data {
						floats[i] = float64(data[i]) / maxInt8
					}

					_, err := n.floats.Write(floats)
					return err
				})

				return n
			},
		},
		{
			name:  "Simple/3Items/WriteOutOfBounds",
			input: []int8{1, 2, -3, -4, 5, 6, -7, 0, 127, -127},
			wants: []float64{0, 1, -1},
			size:  3,
			newFn: func() nested {
				n := nested{
					floats: NewRingFilter[float64](3, nil),
				}

				n.ints = NewRingFilter(3, func(data []int8) error {
					floats := make([]float64, len(data))
					for i := range data {
						floats[i] = float64(data[i]) / maxInt8
					}

					_, err := n.floats.Write(floats)
					return err
				})

				return n
			},
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			buf := testcase.newFn()

			intReader := NewReader(testcase.input)

			_, err := buf.ints.ReadFrom(intReader)
			require.NoError(t, err)

			output := make([]float64, testcase.size)
			_, err = buf.floats.Read(output)
			require.NoError(t, err)
			require.Equal(t, testcase.wants, output)
		})
	}
}
