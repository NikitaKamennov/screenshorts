//go:build windows

package platform

import (
	"context"
	"fmt"
	"os"
	"unsafe"
)

type windowsFocus struct { window uintptr; pid uint32 }

func RememberFocus(ctx context.Context) FocusTarget {
	if ctx.Err() != nil { return nil }
	window, _, _ := user32.NewProc("GetForegroundWindow").Call()
	if window == 0 { return nil }
	var pid uint32
	user32.NewProc("GetWindowThreadProcessId").Call(window, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 || pid == uint32(os.Getpid()) { return nil }
	return windowsFocus{window:window, pid:pid}
}

func (target windowsFocus) Restore(ctx context.Context) error {
	if err := ctx.Err(); err != nil { return err }
	var pid uint32
	user32.NewProc("GetWindowThreadProcessId").Call(target.window, uintptr(unsafe.Pointer(&pid)))
	if pid != target.pid { return fmt.Errorf("Прежнее окно уже закрыто") }
	if foreground, _, _ := user32.NewProc("GetForegroundWindow").Call(); foreground == target.window { return nil }
	if iconic, _, _ := user32.NewProc("IsIconic").Call(target.window); iconic != 0 { user32.NewProc("ShowWindowAsync").Call(target.window, 9) }
	if result, _, _ := user32.NewProc("SetForegroundWindow").Call(target.window); result == 0 {
		return fmt.Errorf("Windows не подтвердила возврат фокуса; снимок сохранён")
	}
	return nil
}
