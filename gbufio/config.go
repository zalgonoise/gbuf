package gbufio

import "github.com/zalgonoise/cfg"

type Config[T any] struct {
	size     int
	delim    T
	excluded *T
}

func defaultConfig[T any]() Config[T] {
	return Config[T]{
		size: defaultBufSize,
	}
}

func WithSize[T any](size int) cfg.Option[Config[T]] {
	return cfg.Register[Config[T]](func(c Config[T]) Config[T] {
		c.size = size

		return c
	})
}

func WithDelim[T comparable](delim T) cfg.Option[Config[T]] {
	return cfg.Register[Config[T]](func(c Config[T]) Config[T] {
		c.delim = delim

		return c
	})
}

func WithExcluded[T comparable](excluded *T) cfg.Option[Config[T]] {
	if excluded == nil {
		return cfg.NoOp[Config[T]]{}
	}

	return cfg.Register[Config[T]](func(c Config[T]) Config[T] {
		c.excluded = excluded

		return c
	})
}
