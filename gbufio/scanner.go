package gbufio

import (
	"errors"
	"io"
	"slices"

	"github.com/zalgonoise/cfg"
	"github.com/zalgonoise/gio"
)

// Scanner provides a convenient interface for reading data such as
// a file of newline-delimited lines of text. Successive calls to
// the Scan method will step through the 'tokens' of a file, skipping
// the bytes between the tokens. The specification of a token is
// defined by a split function of type SplitFunc; the default split
// function breaks the input into lines with line termination stripped. Split
// functions are defined in this package for scanning a file into
// lines, bytes, UTF-8-encoded runes, and space-delimited words. The
// client may instead provide a custom split function.
//
// Scanning stops unrecoverably at EOF, the first I/O error, or a token too
// large to fit in the buffer. When a scan stops, the reader may have
// advanced arbitrarily far past the last token. Programs that need more
// control over error handling or large tokens, or must run sequential scans
// on a reader, should use bufio.Reader instead.
type Scanner[T comparable] struct {
	r     gio.Reader[T] // The reader provided by the client.
	split SplitFunc[T]  // The function to split the tokens.

	delim        T     // Delimiter to use when splitting reads
	excluded     *T    // Excluded value to remove, usually next to the delimiter. Ignored if nil.
	maxTokenSize int   // Maximum size of a token; modified by tests.
	token        []T   // Last token returned by split.
	buf          []T   // Buffer used as argument to split.
	start        int   // First non-processed byte in buf.
	end          int   // End of data in buf.
	err          error // Sticky error.
	empties      int   // Count of successive empty tokens.
	scanCalled   bool  // Scan has been called; buffer is in use.
	done         bool  // Scan has finished.
}

// SplitFunc is the signature of the split function used to tokenize the
// input. The arguments are an initial substring of the remaining unprocessed
// data and a flag, atEOF, that reports whether the Reader has no more data
// to give. The return values are the number of bytes to advance the input
// and the next token to return to the user, if any, plus an error, if any.
//
// Scanning stops if the function returns an error, in which case some of
// the input may be discarded. If that error is ErrFinalToken, scanning
// stops with no error.
//
// Otherwise, the Scanner advances the input. If the token is not nil,
// the Scanner returns it to the user. If the token is nil, the
// Scanner reads more data and continues scanning; if there is no more
// data--if atEOF was true--the Scanner returns. If the data does not
// yet hold a complete token, for instance if it has no newline while
// scanning lines, a SplitFunc can return (0, nil, nil) to signal the
// Scanner to read more data into the slice and try again with a
// longer slice starting at the same point in the input.
//
// The function is never called with an empty data slice unless atEOF
// is true. If atEOF is true, however, data may be non-empty and,
// as always, holds unprocessed text.
type SplitFunc[T comparable] func(data []T, delim T, excluded *T, atEOF bool) (advance int, token []T, err error)

// Errors returned by Scanner.
var (
	ErrTooLong         = errors.New("gbufio.Scanner: token too long")
	ErrNegativeAdvance = errors.New("gbufio.Scanner: SplitFunc returns negative advance count")
	ErrAdvanceTooFar   = errors.New("gbufio.Scanner: SplitFunc returns advance count beyond input")
	ErrBadReadCount    = errors.New("gbufio.Scanner: Read returned impossible count")
)

const (
	// MaxScanTokenSize is the maximum size used to buffer a token
	// unless the user provides an explicit buffer with Scanner.Buffer.
	// The actual maximum token size may be smaller as the buffer
	// may need to include, for instance, a newline.
	MaxScanTokenSize = 64 * 1024

	startBufSize = 4096 // Size of initial allocation for buffer.
)

// NewScanner returns a new Scanner to read from r.
// The split function defaults to ScanLines.
func NewScanner[T comparable](r gio.Reader[T], opts ...cfg.Option[Config[T]]) *Scanner[T] {
	config := cfg.New(opts...)

	return &Scanner[T]{
		r:            r,
		split:        ScanLines[T],
		delim:        config.delim,
		excluded:     config.excluded,
		maxTokenSize: MaxScanTokenSize,
	}
}

// Err returns the first non-EOF error that was encountered by the Scanner.
func (s *Scanner[T]) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

// Value returns the most recent token generated by a call to Scan.
// The underlying array may point to data that will be overwritten
// by a subsequent call to Scan. It does no allocation.
func (s *Scanner[T]) Value() []T {
	return s.token
}

// ErrFinalToken is a special sentinel error value. It is intended to be
// returned by a Split function to indicate that the token being delivered
// with the error is the last token and scanning should stop after this one.
// After ErrFinalToken is received by Scan, scanning stops with no error.
// The value is useful to stop processing early or when it is necessary to
// deliver a final empty token. One could achieve the same behavior
// with a custom error value but providing one here is tidier.
// See the emptyFinalToken example for a use of this value.
var ErrFinalToken = errors.New("final token")

// Scan advances the Scanner to the next token, which will then be
// available through the Bytes or Text method. It returns false when the
// scan stops, either by reaching the end of the input or an error.
// After Scan returns false, the Err method will return any error that
// occurred during scanning, except that if it was io.EOF, Err
// will return nil.
// Scan panics if the split function returns too many empty
// tokens without advancing the input. This is a common error mode for
// scanners.
func (s *Scanner[T]) Scan() bool {
	if s.done {
		return false
	}
	s.scanCalled = true
	// Loop until we have a token.
	for {
		// See if we can get a token with what we already have.
		// If we've run out of data but have an error, give the split function
		// a chance to recover any remaining, possibly empty token.
		if s.end > s.start || s.err != nil {
			advance, token, err := s.split(s.buf[s.start:s.end], s.delim, s.excluded, s.err != nil)
			if err != nil {
				if errors.Is(err, ErrFinalToken) {
					s.token = token
					s.done = true
					return true
				}
				s.setErr(err)
				return false
			}
			if !s.advance(advance) {
				return false
			}
			s.token = token
			if token != nil {
				if s.err == nil || advance > 0 {
					s.empties = 0
				} else {
					// Returning tokens not advancing input at EOF.
					s.empties++
					if s.empties > maxConsecutiveEmptyReads {
						panic("bufio.Scan: too many empty tokens without progressing")
					}
				}
				return true
			}
		}
		// We cannot generate a token with what we are holding.
		// If we've already hit EOF or an I/O error, we are done.
		if s.err != nil {
			// Shut it down.
			s.start = 0
			s.end = 0
			return false
		}
		// Must read more data.
		// First, shift data to beginning of buffer if there's lots of empty space
		// or space is needed.
		if s.start > 0 && (s.end == len(s.buf) || s.start > len(s.buf)/2) {
			copy(s.buf, s.buf[s.start:s.end])
			s.end -= s.start
			s.start = 0
		}
		// Is the buffer full? If so, resize.
		if s.end == len(s.buf) {
			// Guarantee no overflow in the multiplication below.
			const maxInt = int(^uint(0) >> 1)
			if len(s.buf) >= s.maxTokenSize || len(s.buf) > maxInt/2 {
				s.setErr(ErrTooLong)
				return false
			}
			newSize := len(s.buf) * 2
			if newSize == 0 {
				newSize = startBufSize
			}
			if newSize > s.maxTokenSize {
				newSize = s.maxTokenSize
			}
			newBuf := make([]T, newSize)
			copy(newBuf, s.buf[s.start:s.end])
			s.buf = newBuf
			s.end -= s.start
			s.start = 0
		}
		// Finally we can read some input. Make sure we don't get stuck with
		// a misbehaving Reader. Officially we don't need to do this, but let's
		// be extra careful: Scanner is for safe, simple jobs.
		for loop := 0; ; {
			n, err := s.r.Read(s.buf[s.end:len(s.buf)])
			if n < 0 || len(s.buf)-s.end < n {
				s.setErr(ErrBadReadCount)
				break
			}
			s.end += n
			if err != nil {
				s.setErr(err)
				break
			}
			if n > 0 {
				s.empties = 0
				break
			}
			loop++
			if loop > maxConsecutiveEmptyReads {
				s.setErr(io.ErrNoProgress)
				break
			}
		}
	}
}

// advance consumes n bytes of the buffer. It reports whether the advance was legal.
func (s *Scanner[T]) advance(n int) bool {
	if n < 0 {
		s.setErr(ErrNegativeAdvance)
		return false
	}
	if n > s.end-s.start {
		s.setErr(ErrAdvanceTooFar)
		return false
	}
	s.start += n
	return true
}

// setErr records the first error encountered.
func (s *Scanner[T]) setErr(err error) {
	if s.err == nil || s.err == io.EOF {
		s.err = err
	}
}

// Buffer sets the initial buffer to use when scanning and the maximum
// size of buffer that may be allocated during scanning. The maximum
// token size is the larger of max and cap(buf). If max <= cap(buf),
// Scan will use this buffer only and do no allocation.
//
// By default, Scan uses an internal buffer and sets the
// maximum token size to MaxScanTokenSize.
//
// Buffer panics if it is called after scanning has started.
func (s *Scanner[T]) Buffer(buf []T, max int) {
	if s.scanCalled {
		panic("Buffer called after Scan")
	}
	s.buf = buf[0:cap(buf)]
	s.maxTokenSize = max
}

// Split sets the split function for the Scanner.
// The default split function is ScanLines.
//
// Split panics if it is called after scanning has started.
func (s *Scanner[T]) Split(split SplitFunc[T]) {
	if s.scanCalled {
		panic("Split called after Scan")
	}
	s.split = split
}

// Split functions

// dropExcluded drops a specific excluded value from the data, if set.
func dropExcluded[T comparable](data []T, zero *T) []T {
	if len(data) > 0 && *zero != nil && data[len(data)-1] == *zero {
		return data[0 : len(data)-1]
	}
	return data
}

// ScanLines is a split function for a Scanner that returns each line of
// text, stripped of any trailing end-of-line marker. The returned line may
// be empty. The end-of-line marker is one optional carriage return followed
// by one mandatory newline. In regular expression notation, it is `\r?\n`.
// The last non-empty line of input will be returned even if it has no
// newline.
func ScanLines[T comparable](data []T, delim T, excluded *T, atEOF bool) (advance int, token []T, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := slices.Index(data, delim); i >= 0 {
		// We have a full newline-terminated line.
		return i + 1, dropExcluded(data[0:i], excluded), nil
	}
	// If we're at EOF, we have a final, non-terminated line. Return it.
	if atEOF {
		return len(data), dropExcluded(data, excluded), nil
	}
	// Request more data.
	return 0, nil, nil
}
