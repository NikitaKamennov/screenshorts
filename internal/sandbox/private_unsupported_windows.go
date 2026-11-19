//go:build windows && !amd64

package sandbox

import "fmt"

func prepareDirectory(string) error { return fmt.Errorf("Защищённые Windows workers требуют x64") }
