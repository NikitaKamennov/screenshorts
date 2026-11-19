//go:build !windows

package sandbox

import "os"

func prepareDirectory(directory string) error { return os.Chmod(directory, 0700) }
