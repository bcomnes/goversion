// Package main implements a CLI tool to version and publish Go modules.
package main

import (
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, output, errorOutput io.Writer) int {
	if len(arguments) > 0 && arguments[0] == "publish" {
		return runPublishCommand(arguments[1:], output, errorOutput)
	}
	return runVersionCommand(arguments, output, errorOutput)
}
