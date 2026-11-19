//go:build windows && amd64

package sandbox

import "skrinpolka/internal/sandboxwin"

func prepareDirectory(directory string) error { return sandboxwin.PrivateDirectory(directory) }
