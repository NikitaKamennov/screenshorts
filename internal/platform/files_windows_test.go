//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

func TestWindowsFileClipboard(t *testing.T) {
	if os.Getenv("SCREENSHOOTER_WINDOWS_NATIVE_TESTS") != "1" {
		t.Skip("opt-in native Windows clipboard test")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class, _ := syscall.UTF16PtrFromString("STATIC")
	owner, _, err := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	if owner == 0 {
		t.Fatalf("clipboard owner: %v", err)
	}
	windowsByHandle.Store(owner, nil)
	defer func() { windowsByHandle.Delete(owner); user32.NewProc("DestroyWindow").Call(owner) }()
	path := filepath.Join(t.TempDir(), "тест копирования.png")
	if err := os.WriteFile(path, []byte("synthetic clipboard fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CopyFiles([]string{path}); err != nil {
		t.Fatal(err)
	}
	if ok, _, err := user32.NewProc("OpenClipboard").Call(owner); ok == 0 {
		t.Fatal(err)
	}
	defer user32.NewProc("CloseClipboard").Call()
	defer user32.NewProc("EmptyClipboard").Call()
	hdrop, _, _ := user32.NewProc("GetClipboardData").Call(15)
	count, _, _ := shell32.NewProc("DragQueryFileW").Call(hdrop, 0xffffffff, 0, 0)
	if count != 1 {
		t.Fatalf("file count: %d", count)
	}
	name := make([]uint16, 32768)
	shell32.NewProc("DragQueryFileW").Call(hdrop, 0, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
	if syscall.UTF16ToString(name) != path {
		t.Fatal("Unicode path changed in CF_HDROP")
	}
	formatName, _ := syscall.UTF16PtrFromString("Preferred DropEffect")
	format, _, _ := user32.NewProc("RegisterClipboardFormatW").Call(uintptr(unsafe.Pointer(formatName)))
	handle, _, _ := user32.NewProc("GetClipboardData").Call(format)
	pointer, _, _ := kernel32.NewProc("GlobalLock").Call(handle)
	if pointer == 0 {
		t.Fatal("copy preference missing")
	}
	effect := *(*uint32)(unsafe.Pointer(pointer))
	kernel32.NewProc("GlobalUnlock").Call(handle)
	if effect != 1 {
		t.Fatalf("expected COPY, got %d", effect)
	}
}
