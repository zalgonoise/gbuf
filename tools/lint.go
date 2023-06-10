// to run any of the tools in this package, simply call `go generate ./...` from
// the project's top-level directory.
package main

// run golangci-lint on this project
//
// the go:generate directive allows running any kind of command
// so, in this case, we can leverage it to run the linter as a `go run` command
// with a specified version in go.mod
//go:generate go run github.com/golangci/golangci-lint/cmd/golangci-lint run ..
