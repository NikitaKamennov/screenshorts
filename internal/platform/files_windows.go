//go:build windows

package platform

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// CF_HDROP lets Explorer, mail clients and messengers receive actual files.
func CopyFiles(paths []string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var owner uintptr
	windowsByHandle.Range(func(key, value any) bool { owner = key.(uintptr); return false })
	if owner == 0 {
		return fmt.Errorf("Окно приложения ещё не готово к копированию")
	}
	words := []uint16{}
	for _, path := range paths {
		words = append(words, utf16.Encode([]rune(path))...)
		words = append(words, 0)
	}
	words = append(words, 0)
	data := make([]byte, 20+len(words)*2)
	binary.LittleEndian.PutUint32(data, 20)
	binary.LittleEndian.PutUint32(data[16:], 1)
	for i, word := range words {
		binary.LittleEndian.PutUint16(data[20+i*2:], word)
	}
	handle, _, err := kernel32.NewProc("GlobalAlloc").Call(0x42, uintptr(len(data)))
	if handle == 0 {
		return fmt.Errorf("GlobalAlloc: %w", err)
	}
	owned := true
	defer func() {
		if owned {
			kernel32.NewProc("GlobalFree").Call(handle)
		}
	}()
	pointer, _, err := kernel32.NewProc("GlobalLock").Call(handle)
	if pointer == 0 {
		return fmt.Errorf("GlobalLock: %w", err)
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(pointer)), len(data)), data)
	kernel32.NewProc("GlobalUnlock").Call(handle)
	// Explicit COPY preference prevents a destination choosing a move based
	// on its default context when pasting files from the same volume.
	formatName, _ := syscall.UTF16PtrFromString("Preferred DropEffect")
	format, _, err := user32.NewProc("RegisterClipboardFormatW").Call(uintptr(unsafe.Pointer(formatName)))
	if format == 0 {
		return fmt.Errorf("RegisterClipboardFormatW: %w", err)
	}
	effect, _, err := kernel32.NewProc("GlobalAlloc").Call(0x42, 4)
	if effect == 0 {
		return fmt.Errorf("GlobalAlloc: %w", err)
	}
	effectOwned := true
	defer func() {
		if effectOwned {
			kernel32.NewProc("GlobalFree").Call(effect)
		}
	}()
	effectPointer, _, err := kernel32.NewProc("GlobalLock").Call(effect)
	if effectPointer == 0 {
		return fmt.Errorf("GlobalLock: %w", err)
	}
	*(*uint32)(unsafe.Pointer(effectPointer)) = 1 // DROPEFFECT_COPY
	kernel32.NewProc("GlobalUnlock").Call(effect)
	if ok, _, err := user32.NewProc("OpenClipboard").Call(owner); ok == 0 {
		return fmt.Errorf("Буфер занят другим приложением; повтори копирование: %w", err)
	}
	defer user32.NewProc("CloseClipboard").Call()
	if ok, _, err := user32.NewProc("EmptyClipboard").Call(); ok == 0 {
		return fmt.Errorf("EmptyClipboard: %w", err)
	}
	if ok, _, err := user32.NewProc("SetClipboardData").Call(15, handle); ok == 0 {
		return fmt.Errorf("SetClipboardData: %w", err)
	}
	owned = false
	if ok, _, err := user32.NewProc("SetClipboardData").Call(format, effect); ok == 0 {
		user32.NewProc("EmptyClipboard").Call()
		return fmt.Errorf("SetClipboardData(copy): %w", err)
	}
	effectOwned = false
	return nil
}

func OpenFolder(path string) error {
	verb, _ := syscall.UTF16PtrFromString("open")
	target, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	result, _, _ := shell32.NewProc("ShellExecuteW").Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
	if result <= 32 {
		return fmt.Errorf("Не удалось открыть папку: Windows %d", result)
	}
	return nil
}
