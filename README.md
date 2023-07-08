# gbuf

*A generic buffer library for Go*

_________


## Overview

Similar to [`zalgonoise/gio`](https://github.com/zalgonoise/gio), this library extends the usefulness of the interfaces and functions exposed by the standard library (for byte buffers) with generics. The core functionality of a reader and buffer (something that reads) should be common amongst any type, provided that the implementation can handle the calls (to read, to write, or whatever action).

This way, despite not having the same fluidity as the standard library's implementation in some levels (there is no type-specific method such as `WriteString` and `WriteByte`), it promises to allow the same API to be transposed to other defined types.

Other than this, all functionality from the standard library is present in this generic I/O library.

### Why generics?

Generics are great for when there is a solid algorithm that serves for many types, and can be abstracted enough to work without major workarounds; and this approach to a buffers library is very idiomatic and so simple (the Go way). Of course, the standard library's implementation has some other packages in mind that work together with `bytes`, namely UTF-encoding and properly handling runes. The approach with generics will limit the potential that shines in the original implementation, one way or the other (simply with the fact that if you need handle different types, you need to convert them yourself).

But all in all, it was a great exercise to practice using generics. Maybe I will just use this library once or twice, maybe it will be actually useful for some. I am just in it for the ride. :)


## Disclaimer

This library will mirror all logic from Go's (standard) `bytes` and `container` libraries; and change the `[]byte` implementation with a generic `T any` and `[]T` implementation. There are no changes in the actual logic in the library.
________________

### Added Features

Besides recently adding `container` library generic implementations (heap, list and ring); I've also extended the concept of the circular buffer with the `RingBuffer` type, that is a circular buffer with an `io.Reader` / `io.Writer` implementation (and goodies). Another type similar to this one is `RingFilter` which allows passing a slice of the unread items on each cycle to a given `func([]T) error` -- that allows filtering data / chaining readers / building data processing pipelines.

#### [`RingBuffer`](./ringbuffer.go) 

Acts as a circular buffer, with the same API as a [`Buffer`](./buffer.go) type. The caller defines a buffer size (and type), where all writes happen within that buffer size, wrapping around the buffer if needed. This brings the option of having a buffer that does not allocate any more memory than originally defined, if the caller is OK with discarding items if overwritten (for example, a floating-point audio signal that is not meant to be persisted or stored).

```go
  const size = 5
  buf := gbuf.NewRingBuffer[byte](size) // create a bytes RingBuffer, of size 5

  buf.Write([]byte("some string")) // buffer stores "tring"
  
  output := make([]byte, size)
  _, _ = buf.Read(output) // buffer reads "tring"
```

#### [`RingFilter`](./ringfilter.go)

Similar to RingBuffer, this type allows configuring a processing function that is called on every write. The buffer still stores the data the exact same way that the RingBuffer does, however all written items are also fed through this filter function.

Extending the RingBuffer to have this processing function allows to create fast type converters without spending any excessive allocations. A use-case for this type is to convert an audio signal (in bytes) into its floating-point representation, while only keeping the latest written audio data as defined in the buffer size.

```go
  // max int8 value used to convert 8bit PCM audio into its floating-point representation
  const maxInt8 float64 = 1<<7 - 1

  // create a type containing both buffers
  type converter struct {
      ints   *gbuf.RingFilter[int8]
      floats *gbuf.RingFilter[float64]
  }
  
  // create the "output" buffer, in this case for float64, with a certain size
  conv := converter{
	  floats: gbuf.NewRingFilter[float64](3, nil),
  }

  // create the "input" buffer, in this case int8, that happens to be
  // the same size (one byte per sample, on 8bit PCM audio data) 
  conv.ints = gbuf.NewRingFilter(3, func(data []int8) error {
	  // in this filter function, we convert the read data into its floating-point representation	  
	  floats := make([]float64, len(data))
	  for i := range data {
	      floats[i] = float64(data[i]/ maxInt8)	  
      }

	  // then we write that data into the float buffer
	  _, err := conv.floats.Write(floats)
	  return err
  })
```

Of course there are many open applications to these buffers, and possibly even new buffer types in the future.