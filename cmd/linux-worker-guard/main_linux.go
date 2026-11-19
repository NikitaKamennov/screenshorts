//go:build linux

// A static entry point: systemd never needs GTK/WebKit to establish worker limits.
package main

import (
	"os"
	"skrinpolka/internal/sandbox"
)

func main() {
	if handled, code := sandbox.WorkerGuard(os.Args[1:]); handled {
		os.Exit(code)
	}
	os.Exit(125)
}
