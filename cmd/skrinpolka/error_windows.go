//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Preserve a visible startup error when Windows is built with windowsgui.
func reportStartupError(err error) {
	fmt.Fprintln(os.Stderr, "ScreenShorts Pro:", err)
	message, messageErr := syscall.UTF16PtrFromString(err.Error())
	title, titleErr := syscall.UTF16PtrFromString("ScreenShorts Pro — ошибка запуска")
	if messageErr != nil || titleErr != nil {
		return
	}
	syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(
		0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), 0x10,
	)
}
