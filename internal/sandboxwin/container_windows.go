//go:build windows && amd64

// Adapted from Zvukbook (MIT); see licenses/zvukbook/LICENSE.
package sandboxwin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var userenv = syscall.NewLazyDLL("userenv.dll")
var advapi = syscall.NewLazyDLL("advapi32.dll")

type securityCapabilities struct {
	SID             unsafe.Pointer
	Capabilities    unsafe.Pointer
	Count, Reserved uint32
}

type Container struct {
	capabilities securityCapabilities
	sidText      string
	name         string
	grants       []string
}

func icacls(ctx context.Context, path string, args ...string) error {
	command := exec.CommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "icacls.exe"), append([]string{path}, args...)...)
	command.WaitDelay = time.Second
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("icacls %s: %w: %s", path, err, out)
	}
	return nil
}

func NewContainer(ctx context.Context, name string, readRoots []string, output string) (*Container, error) {
	if err := ctx.Err(); err != nil { return nil, err }
	moniker, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	c := &Container{name: name}
	hresult, _, _ := userenv.NewProc("CreateAppContainerProfile").Call(uintptr(unsafe.Pointer(moniker)),
		uintptr(unsafe.Pointer(moniker)), uintptr(unsafe.Pointer(moniker)), 0, 0, uintptr(unsafe.Pointer(&c.capabilities.SID)))
	if uint32(hresult) != 0 {
		return nil, fmt.Errorf("CreateAppContainerProfile HRESULT %#x", uint32(hresult))
	}
	var sidString *uint16
	if err = winOK(advapi.NewProc("ConvertSidToStringSidW"), uintptr(c.capabilities.SID), uintptr(unsafe.Pointer(&sidString))); err != nil {
		return nil, errors.Join(err, c.Close())
	}
	count := 0
	for *(*uint16)(unsafe.Add(unsafe.Pointer(sidString), count*2)) != 0 {
		count++
	}
	c.sidText = syscall.UTF16ToString(unsafe.Slice(sidString, count))
	kernel.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(sidString)))
	for _, root := range readRoots {
		if err = c.grant(ctx, root, "RX"); err != nil {
			return nil, errors.Join(err, c.Close())
		}
	}
	if err = icacls(ctx, output, "/setintegritylevel", "(OI)(CI)L"); err == nil {
		err = c.grant(ctx, output, "M")
	}
	if err != nil {
		return nil, errors.Join(err, c.Close())
	}
	return c, nil
}

func (c *Container) grant(ctx context.Context, path, access string) error {
	if err := ctx.Err(); err != nil { return err }
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	inherit := ""
	if info.IsDir() {
		inherit = "(OI)(CI)"
	}
	if err = icacls(ctx, path, "/grant", "*"+c.sidText+":"+inherit+"("+access+")"); err != nil {
		return err
	}
	c.grants = append(c.grants, path)
	return nil
}

func (c *Container) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var result error
	for i := len(c.grants) - 1; i >= 0; i-- {
		result = errors.Join(result, icacls(ctx, c.grants[i], "/remove:g", "*"+c.sidText))
	}
	c.grants = nil
	if c.capabilities.SID != nil {
		advapi.NewProc("FreeSid").Call(uintptr(c.capabilities.SID))
		c.capabilities.SID = nil
	}
	if c.name != "" {
		moniker, _ := syscall.UTF16PtrFromString(c.name)
		code, _, _ := userenv.NewProc("DeleteAppContainerProfile").Call(uintptr(unsafe.Pointer(moniker)))
		if uint32(code) != 0 { result = errors.Join(result, fmt.Errorf("DeleteAppContainerProfile HRESULT %#x", uint32(code))) }
		c.name = ""
	}
	return result
}
