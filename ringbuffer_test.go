package gbuf

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRingBuffer_Write(t *testing.T) {
	for _, testcase := range []struct {
		name  string
		input string
		size  int
		wants string
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
			wants: "x\x00\x00\x00\x00\x00\x00\x00\x00\x00",
		},
		{
			name:  "ByteAtATime",
			input: "one byte at a time",
			size:  1,
			wants: "e",
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			output := make([]byte, testcase.size)
			r := NewRingBuffer[byte](testcase.size)

			_, err := r.Write([]byte(testcase.input))
			require.NoError(t, err)
			_, err = r.Read(output)
			require.NoError(t, err)

			require.Equal(t, testcase.wants, string(output))
		})
	}
}

func TestRingBuffer_Write_Sequential(t *testing.T) {
	for _, testcase := range []struct {
		name       string
		input      string
		size       int
		chunkSizes []int
		wants      string
	}{
		{
			name:       "Simple",
			input:      "very long string buffered every 5 characters",
			size:       5,
			chunkSizes: []int{5, 5, 5, 5, 5, 5, 5, 5, 4},
			wants:      "cters",
		},
		{
			name:       "Short",
			input:      "x",
			size:       10,
			chunkSizes: []int{1},
			wants:      "x\x00\x00\x00\x00\x00\x00\x00\x00\x00",
		},
		{
			name:       "ByteAtATime",
			input:      "one byte",
			size:       1,
			chunkSizes: []int{1, 1, 1, 1, 1, 1, 1, 1},
			wants:      "e",
		},
		{
			name:       "InconsistentWrite",
			input:      "a very long string that is stuck on the streaming wheel, clearly",
			size:       5,
			chunkSizes: []int{3, 12, 5, 20, 10, 14},
			wants:      "lyear",
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			var output = make([]byte, testcase.size)
			var n int

			r := NewRingBuffer[byte](testcase.size)

			for _, size := range testcase.chunkSizes {
				_, err := r.Write([]byte(testcase.input)[n : n+size])
				require.NoError(t, err)
				n += size
			}

			_, err := r.Read(output)
			require.NoError(t, err)

			require.Equal(t, testcase.wants, string(output))
		})
	}
}

func TestRingBuffer_Read(t *testing.T) {
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
			r := NewRingBuffer[byte](testcase.size)

			_, err := r.Write([]byte(testcase.input))
			require.NoError(t, err)

			buf := make([]byte, testcase.readSize)
			_, err = r.Read(buf)
			require.ErrorIs(t, err, testcase.err)
			require.Equal(t, testcase.wants, string(buf))
		})
	}
}

func TestRingBuffer_WriteRead_Interleaved(t *testing.T) {
	type writeRead struct {
		write, read int
	}

	for _, testcase := range []struct {
		name       string
		input      string
		wants      string
		wantsRead  string
		size       int
		chunkSizes []writeRead // [write, read] pairs of operations
	}{
		{
			name:  "WriteWithSomeReads",
			input: "very long string buffered every 5 characters",
			size:  10,
			chunkSizes: []writeRead{
				{5, 0}, {3, 0}, {0, 4}, {7, 0},
			},
			wants:     "long strin",
			wantsRead: "very",
		},
		{
			name:  "WritesSweepThroughReads",
			input: "very long string buffered every 5 characters",
			size:  10,
			chunkSizes: []writeRead{
				{8, 3}, {8, 4}, {8, 0},
			},
			wants:     "ng buffere",
			wantsRead: "verong ",
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			var output = make([]byte, testcase.size)
			var outputRead = make([]byte, len(testcase.input))
			var writeN int
			var readN int

			r := NewRingBuffer[byte](testcase.size)

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

			_, err := r.Read(output)
			require.NoError(t, err)

			require.Equal(t, testcase.wants, string(output))
			require.Equal(t, testcase.wantsRead, string(outputRead))
		})
	}
}

func BenchmarkRingBuffer_Write(b *testing.B) {
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
			r := NewRingBuffer[byte](testcase.size)

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

func TestRingBuffer_ReadFrom_Read(t *testing.T) {
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
			r := NewRingBuffer[byte](testcase.size)

			_, err := r.ReadFrom(bytes.NewReader([]byte(testcase.input)))
			require.NoError(t, err)

			var read = make([]byte, testcase.size)
			n, err := r.Read(read)
			require.Greater(t, n, 0)
			require.NoError(t, err)
			require.Equal(t, testcase.wants, string(read))
		})
	}
}

func TestRingBuffer_ReadFrom_Interleaved(t *testing.T) {
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

			r := NewRingBuffer[byte](testcase.size)

			var swap bool

			for i := range testcase.input {
				if swap {
					_, err := r.Write([]byte(testcase.input[i]))
					require.NoError(t, err)

					swap = !swap
					continue
				}

				_, err := r.ReadFrom(bytes.NewReader([]byte(testcase.input[i])))
				require.NoError(t, err)
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

func TestRingBuffer_WriteTo(t *testing.T) {
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
			r := NewRingBuffer[byte](testcase.size)

			_, err := r.Write([]byte(testcase.input))
			require.NoError(t, err)

			buf := bytes.NewBuffer(make([]byte, 0, testcase.size))
			_, err = r.WriteTo(buf)
			require.ErrorIs(t, err, testcase.err)
			require.Equal(t, testcase.wants, buf.String())
		})
	}
}

func TestRingBuffer_Value(t *testing.T) {
	for _, testcase := range []struct {
		name     string
		input    []int
		readPos  int
		writePos int
		wants    []int
	}{
		{
			name:  "Simple/ReadWriteAtZero",
			input: []int{1, 2, 3, 4, 5, 6, 7, 8, 9},
			wants: []int{1, 2, 3, 4, 5, 6, 7, 8, 9},
		},
		{
			name:     "Simple/ReadThreeItems",
			input:    []int{1, 2, 3, 4, 5, 6, 7, 8, 9},
			writePos: 3,
			wants:    []int{1, 2, 3},
		},
		{
			name:     "Simple/RoundTripAtMidPoint",
			input:    []int{1, 2, 3, 4, 5, 6, 7, 8, 9},
			readPos:  3,
			writePos: 3,
			wants:    []int{4, 5, 6, 7, 8, 9, 1, 2, 3},
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			buf := NewRingBuffer[int](len(testcase.input))
			_, err := buf.Write(testcase.input)
			require.NoError(t, err)

			buf.read = testcase.readPos
			buf.write = testcase.writePos

			output := buf.Value()
			require.Equal(t, testcase.wants, output)
		})
	}
}

func TestRingBuffer_WriteItem_ReadItem_Read(t *testing.T) {
	for _, testcase := range []struct {
		name       string
		size       int
		writeItems []byte
		readItems  []byte
		wants      string
	}{
		{
			name:       "Simple/NoReads",
			size:       3,
			writeItems: []byte("input"),
			wants:      "put",
		},
		{
			name:       "Simple/WithReads",
			size:       5,
			writeItems: []byte("input"),
			readItems:  []byte("in"),
			wants:      "put\x00\x00",
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			buf := NewRingBuffer[byte](testcase.size)

			for i := range testcase.writeItems {
				err := buf.WriteItem(testcase.writeItems[i])
				require.NoError(t, err)
			}

			readItems := make([]byte, 0, len(testcase.readItems))
			for range testcase.readItems {
				item, err := buf.ReadItem()
				require.NoError(t, err)
				readItems = append(readItems, item)
			}

			require.Equal(t, string(testcase.readItems), string(readItems))

			output := make([]byte, testcase.size)
			_, err := buf.Read(output)
			require.NoError(t, err)

			require.Equal(t, testcase.wants, string(output))
		})
	}
}

func TestRingBuffer_UnreadItem(t *testing.T) {
	for _, testcase := range []struct {
		name       string
		size       int
		input      []byte
		numReads   int
		numUnreads int
		wants      string
		err        error
	}{
		{
			name:       "Simple/Success/ReadTwice_UnreadOnce",
			size:       5,
			input:      []byte("input"),
			numReads:   2,
			numUnreads: 1,
			wants:      "nput\x00",
		},
		{
			name:       "Simple/Fail/UnreadOnce",
			size:       5,
			input:      []byte("input"),
			numReads:   0,
			numUnreads: 1,
			err:        ErrRingBufferUnreadItem,
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			buf := NewRingBuffer[byte](testcase.size)

			_, err := buf.Write(testcase.input)
			require.NoError(t, err)

			readItems := make([]byte, testcase.numReads)
			_, err = buf.Read(readItems)
			require.NoError(t, err)

			for i := 0; i < testcase.numUnreads; i++ {
				err = buf.UnreadItem()
				if err != nil {
					require.ErrorIs(t, err, testcase.err)
					return
				}
			}

			output := make([]byte, testcase.size)
			_, err = buf.Read(output)
			require.NoError(t, err)
			require.Equal(t, testcase.wants, string(output))
		})
	}
}

func TestRingBuffer_Next(t *testing.T) {
	for _, testcase := range []struct {
		name    string
		size    int
		input   []byte
		numNext int
		wants   string
	}{
		{
			name:    "Simple/Next3Items",
			size:    5,
			input:   []byte("input"),
			numNext: 3,
			wants:   "inp",
		},
		{
			name:    "Simple/Overflow",
			size:    5,
			input:   []byte("input"),
			numNext: 10,
			wants:   "input",
		},
		{
			name:    "Simple/Zero",
			size:    5,
			input:   []byte("input"),
			numNext: 0,
			wants:   "",
		},
		{
			name:    "Simple/Negative",
			size:    5,
			input:   []byte("input"),
			numNext: -1,
			wants:   "",
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			buf := NewRingBuffer[byte](testcase.size)

			_, err := buf.Write(testcase.input)
			require.NoError(t, err)

			output := buf.Next(testcase.numNext)
			require.Equal(t, testcase.wants, string(output))
		})
	}
}

func TestRingBuffer_Truncate(t *testing.T) {
	for _, testcase := range []struct {
		name        string
		size        int
		input       []byte
		numTruncate int
		wants       string
	}{
		{
			name:        "Simple/Truncate2",
			size:        5,
			input:       []byte("input"),
			numTruncate: 2,
			wants:       "put\x00\x00",
		},
		{
			name:        "Simple/TruncateZero",
			size:        5,
			input:       []byte("input"),
			numTruncate: 0,
			wants:       "\x00\x00\x00\x00\x00",
		},
		{
			name:        "Simple/TruncateAll",
			size:        5,
			input:       []byte("input"),
			numTruncate: 5,
			wants:       "\x00\x00\x00\x00\x00",
		},
		{
			name:        "Simple/TruncateOverflow",
			size:        5,
			input:       []byte("input"),
			numTruncate: 10,
			wants:       "\x00\x00\x00\x00\x00",
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			buf := NewRingBuffer[byte](testcase.size)

			_, err := buf.Write(testcase.input)
			require.NoError(t, err)

			buf.Truncate(testcase.numTruncate)

			output := make([]byte, testcase.size)
			_, err = buf.Read(output)

			require.Equal(t, testcase.wants, string(output))
		})
	}
}

func TestRingBuffer_Seek(t *testing.T) {
	for _, testcase := range []struct {
		name       string
		size       int
		input      []byte
		numReads   int
		whence     int
		offset     int64
		wantsAbs   int64
		wantsValue byte
		err        error
	}{
		{
			name:       "Simple/NoReads/SeekCurrentPlusOne",
			size:       5,
			input:      []byte("input"),
			whence:     io.SeekCurrent,
			offset:     1,
			wantsAbs:   1,
			wantsValue: 'n',
		},
		{
			name:       "Simple/WithReads/SeekCurrentPlusOne",
			size:       5,
			input:      []byte("input"),
			numReads:   2,
			whence:     io.SeekCurrent,
			offset:     1,
			wantsAbs:   3,
			wantsValue: 'u',
		},
		{
			name:       "Simple/NoReads/SeekStartPlusOne",
			size:       5,
			input:      []byte("input"),
			whence:     io.SeekStart,
			offset:     1,
			wantsAbs:   1,
			wantsValue: 'n',
		},
		{
			name:       "Simple/WithReads/SeekStartPlusOne",
			size:       5,
			input:      []byte("input"),
			numReads:   2,
			whence:     io.SeekStart,
			offset:     1,
			wantsAbs:   3,
			wantsValue: 'u',
		},
		{
			name:       "Simple/NoReads/SeekEndMinusOne",
			size:       5,
			input:      []byte("inp"),
			whence:     io.SeekEnd,
			offset:     -1,
			wantsAbs:   2,
			wantsValue: 'p',
		},
		{
			name:       "Simple/WithReads/SeekEndMinusOne",
			size:       5,
			input:      []byte("inp"),
			numReads:   1,
			whence:     io.SeekEnd,
			offset:     -1,
			wantsAbs:   2,
			wantsValue: 'p',
		},
		{
			name:   "Simple/Fail/InvalidWhence",
			size:   5,
			input:  []byte("input"),
			whence: 99,
			offset: 1,
			err:    ErrWhence, // generic whence error
		},
		{
			name:       "Overflow/NoReads/SeekCurrentPlusEleven",
			size:       5,
			input:      []byte("input"),
			whence:     io.SeekCurrent,
			offset:     11,
			wantsAbs:   1,
			wantsValue: 'n',
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			buf := NewRingBuffer[byte](testcase.size)

			_, err := buf.Write(testcase.input)
			require.NoError(t, err)

			if testcase.numReads > 0 {
				readBytes := make([]byte, testcase.numReads)

				_, err = buf.Read(readBytes)
				require.NoError(t, err)
			}

			abs, err := buf.Seek(testcase.offset, testcase.whence)
			if err != nil {
				require.ErrorIs(t, err, testcase.err)
				return
			}

			require.Equal(t, testcase.wantsAbs, abs)
			item, err := buf.ReadItem()
			require.NoError(t, err)
			require.Equal(t, testcase.wantsValue, item)
		})
	}
}

func TestRingBuffer_ReadItems(t *testing.T) {
	for _, testcase := range []struct {
		name     string
		size     int
		input    []byte
		numReads int
		delim    func(byte) bool
		wants    string
	}{
		{
			name:  "Simple/NoReads/ReadThree",
			size:  5,
			input: []byte("input"),
			delim: func(b byte) bool {
				return b == 'u'
			},
			wants: "inp",
		},
		{
			name:     "Simple/WithReads/ReadThree",
			size:     5,
			input:    []byte("input"),
			numReads: 2,
			delim: func(b byte) bool {
				return b == 't'
			},
			wants: "pu",
		},
		{
			name:  "Extra/NoReads/HalfwayWritten",
			size:  5,
			input: []byte("inp"),
			delim: func(b byte) bool {
				return b == 'p'
			},
			wants: "in",
		},
		{
			name:  "Extra/NoReads/DelimDoesNotMatch",
			size:  5,
			input: []byte("input"),
			delim: func(b byte) bool {
				return b == 'z'
			},
			wants: "input",
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			buf := NewRingBuffer[byte](testcase.size)

			_, err := buf.Write(testcase.input)
			require.NoError(t, err)

			if testcase.numReads > 0 {
				readItems := make([]byte, testcase.numReads)
				_, err = buf.Read(readItems)
				require.NoError(t, err)
			}

			items, err := buf.ReadItems(testcase.delim)
			require.NoError(t, err)
			require.Equal(t, testcase.wants, string(items))
		})
	}
}

func TestRingBuffer_Cap(t *testing.T) {
	for _, testcase := range []struct {
		name  string
		size  int
		wants int
	}{
		{
			name:  "Simple/Size3",
			size:  3,
			wants: 3,
		},
		{
			name:  "Simple/Size1024",
			size:  1024,
			wants: 1024,
		},
		{
			name:  "Extra/Size0",
			size:  0,
			wants: 256,
		},
		{
			name:  "Extra/NegativeSize",
			size:  -1,
			wants: 256,
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			buf := NewRingBuffer[byte](testcase.size)

			require.Equal(t, testcase.wants, buf.Cap())
		})
	}
}
