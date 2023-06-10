//go:build tools
// +build tools

// to run any of the tools in this package, simply call `go generate ./...` from
// the project's top-level directory.
package main

import (
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
)
