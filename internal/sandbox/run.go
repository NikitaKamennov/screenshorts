// Package sandbox is the only entry point for image/text-processing children.
// Missing isolation is an error; it never launches an unprotected fallback.
package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Spec struct {
	// RuntimeRoot selects a packaged Linux /usr; empty keeps the system adapter.
	RuntimeRoot    string
	Executable     string
	Arguments      []string
	ReadRoots      []string
	Directory      string
	MemoryBytes    uint64
	Threads        int
	TimeoutSeconds int
}

// PrepareDirectory protects a newly created per-job directory before its input
// pixels or output text are written, rather than only before starting a child.
func PrepareDirectory(directory string) error {
	if !filepath.IsAbs(directory) {
		return fmt.Errorf("Рабочий каталог worker требует абсолютный путь")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Нужен собственный каталог задания без ссылки")
	}
	return prepareDirectory(directory)
}

func validate(spec Spec) error {
	if !filepath.IsAbs(spec.Executable) || !filepath.IsAbs(spec.Directory) {
		return fmt.Errorf("Worker требует абсолютные пути")
	}
	if spec.MemoryBytes < 128<<20 || spec.MemoryBytes > 4<<30 || spec.Threads < 1 || spec.Threads > 4 || spec.TimeoutSeconds < 1 || spec.TimeoutSeconds > 300 {
		return fmt.Errorf("Профиль worker выходит за допустимые пределы")
	}
	info, err := os.Stat(spec.Executable)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Локальный движок не найден")
	}
	for _, root := range spec.ReadRoots {
		actual, err := filepath.EvalSymlinks(root)
		if err != nil {
			return fmt.Errorf("Не найден read-only каталог worker")
		}
		if !filepath.IsAbs(actual) || filepath.Clean(actual) == filepath.VolumeName(actual)+string(filepath.Separator) {
			return fmt.Errorf("Нельзя выдавать worker доступ ко всему диску")
		}
		if info, err := os.Stat(actual); err != nil || !info.IsDir() {
			return fmt.Errorf("Read-only корень worker должен быть каталогом")
		}
	}
	for _, arg := range spec.Arguments {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("Некорректный аргумент worker")
		}
	}
	return nil
}

func Run(ctx context.Context, spec Spec) error {
	if err := validate(spec); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeLimit(spec.TimeoutSeconds))
	defer cancel()
	if err := resources(spec); err != nil {
		return err
	}
	return run(ctx, spec)
}
