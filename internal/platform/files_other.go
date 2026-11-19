//go:build !windows && !linux

package platform

import "fmt"

func CopyFiles([]string) error {
	return fmt.Errorf("Копирование файлов на этой ОС не реализовано")
}
func OpenFolder(string) error {
	return fmt.Errorf("Открытие папки на этой ОС не реализовано")
}
