//go:build linux

package platform

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

func CopyFiles(paths []string) error {
	lines := []string{}
	for _, path := range paths {
		lines = append(lines, (&url.URL{Scheme: "file", Path: path}).String())
	}
	var command *exec.Cmd
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		command = exec.Command("wl-copy", "--type", "text/uri-list")
	} else {
		command = exec.Command("xclip", "-selection", "clipboard", "-t", "text/uri-list", "-i")
	}
	command.Stdin = strings.NewReader(strings.Join(lines, "\r\n") + "\r\n")
	if err := command.Run(); err != nil {
		return fmt.Errorf("Для копирования файлов нужен wl-copy (Wayland) или xclip (X11): %w", err)
	}
	return nil
}

func OpenFolder(path string) error {
	command := exec.Command("xdg-open", path)
	if err := command.Start(); err != nil {
		return err
	}
	go command.Wait()
	return nil
}
