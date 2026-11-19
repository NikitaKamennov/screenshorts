//go:build !windows

package main

import (
	"fmt"
	"os"
)

func reportStartupError(err error) {
	fmt.Fprintln(os.Stderr, "ScreenShorts Pro:", err)
}
