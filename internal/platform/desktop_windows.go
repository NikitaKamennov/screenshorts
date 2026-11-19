//go:build windows

package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"runtime"
	"sort"
	"syscall"
	"unsafe"
)

var shcore = syscall.NewLazyDLL("shcore.dll")
var monitorScale = shcore.NewProc("GetScaleFactorForMonitor")
var nativeMonitorProc = syscall.NewCallback(enumerateWindowsMonitor)
var processDPIError error

func perMonitorV2() uintptr { return ^uintptr(3) }

func threadUsesPerMonitorV2() bool {
	get := user32.NewProc("GetThreadDpiAwarenessContext")
	equal := user32.NewProc("AreDpiAwarenessContextsEqual")
	if get.Find() != nil || equal.Find() != nil { return false }
	current, _, _ := get.Call()
	result, _, _ := equal.Call(current, perMonitorV2())
	return current != 0 && result != 0
}

func ConfigureProcess() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	proc := user32.NewProc("SetProcessDpiAwarenessContext")
	if err := proc.Find(); err != nil {
		processDPIError = fmt.Errorf("Windows не предоставляет per-monitor DPI v2: %w", err)
		return
	}
	proc.Call(perMonitorV2())
	// A manifest may have already set the context. The effective context, not
	// a repeated setter's access-denied result, determines availability.
	if !threadUsesPerMonitorV2() { processDPIError = fmt.Errorf("Не подтверждён per-monitor DPI v2; захват отключён, чтобы не сохранять неверную область") }
}

type windowsMonitorInfo struct {
	Size uint32
	Monitor, Work winRect
	Flags uint32
	Device [32]uint16
}

type windowsMonitor struct {
	Device string
	Bounds winRect
	Scale uint32
	Primary bool
}

type windowsDesktop struct {
	x, y, width, height int
	topology string
}

type windowsMonitorEnumeration struct {
	monitors []windowsMonitor
	err error
}

// EnumDisplayMonitors is synchronous: the state pointer is retained only for
// the duration of the call, and the callback itself is registered once.
func enumerateWindowsMonitor(monitor, dc, rectangle, data uintptr) uintptr {
	state := (*windowsMonitorEnumeration)(unsafe.Pointer(data))
	if len(state.monitors) >= 64 { state.err = fmt.Errorf("Слишком много поверхностей дисплеев Windows"); return 0 }
	info := windowsMonitorInfo{}
	info.Size = uint32(unsafe.Sizeof(info))
	if result, _, err := user32.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info))); result == 0 {
		state.err = fmt.Errorf("Не удалось прочитать геометрию монитора: %w", err)
		return 0
	}
	if info.Monitor.Right <= info.Monitor.Left || info.Monitor.Bottom <= info.Monitor.Top {
		state.err = fmt.Errorf("Windows вернула пустые границы монитора")
		return 0
	}
	var scale uint32
	code, _, _ := monitorScale.Call(monitor, uintptr(unsafe.Pointer(&scale)))
	if uint32(code) != 0 || scale < 100 || scale > 500 {
		state.err = fmt.Errorf("Не удалось подтвердить масштаб монитора, HRESULT %#x", uint32(code))
		return 0
	}
	state.monitors = append(state.monitors, windowsMonitor{Device:syscall.UTF16ToString(info.Device[:]), Bounds:info.Monitor, Scale:scale, Primary:info.Flags&1 != 0})
	return 1
}

func readWindowsDesktop() (windowsDesktop, error) {
	desktop := windowsDesktop{x:screenMetric(76), y:screenMetric(77), width:screenMetric(78), height:screenMetric(79)}
	if desktop.width <= 0 || desktop.height <= 0 || int64(desktop.width)*int64(desktop.height) > MaxCapturePixels {
		return windowsDesktop{}, fmt.Errorf("Рабочий стол превышает лимит захвата 32 млн пикселей")
	}
	if !threadUsesPerMonitorV2() { return windowsDesktop{}, fmt.Errorf("Захват требует физических координат per-monitor DPI v2") }
	if err := monitorScale.Find(); err != nil { return windowsDesktop{}, fmt.Errorf("Не удалось определить масштаб дисплеев: %w", err) }
	state := windowsMonitorEnumeration{}
	result, _, err := user32.NewProc("EnumDisplayMonitors").Call(0, 0, nativeMonitorProc, uintptr(unsafe.Pointer(&state)))
	runtime.KeepAlive(&state)
	if state.err != nil { return windowsDesktop{}, state.err }
	if result == 0 { return windowsDesktop{}, fmt.Errorf("Не удалось перечислить мониторы Windows: %w", err) }
	if len(state.monitors) == 0 { return windowsDesktop{}, fmt.Errorf("Нет доступных дисплеев Windows") }
	if desktop.x != screenMetric(76) || desktop.y != screenMetric(77) || desktop.width != screenMetric(78) || desktop.height != screenMetric(79) {
		return windowsDesktop{}, fmt.Errorf("Дисплеи изменились во время чтения геометрии; повтори захват")
	}
	sort.Slice(state.monitors, func(i, j int) bool {
		left, right := state.monitors[i], state.monitors[j]
		if left.Device != right.Device { return left.Device < right.Device }
		if left.Bounds.Left != right.Bounds.Left { return left.Bounds.Left < right.Bounds.Left }
		if left.Bounds.Top != right.Bounds.Top { return left.Bounds.Top < right.Bounds.Top }
		if left.Bounds.Right != right.Bounds.Right { return left.Bounds.Right < right.Bounds.Right }
		if left.Bounds.Bottom != right.Bounds.Bottom { return left.Bounds.Bottom < right.Bounds.Bottom }
		if left.Scale != right.Scale { return left.Scale < right.Scale }
		return !left.Primary && right.Primary
	})
	content, err := json.Marshal(state.monitors)
	if err != nil { return windowsDesktop{}, err }
	digest := sha256.Sum256(content)
	desktop.topology = hex.EncodeToString(digest[:])
	return desktop, nil
}

func (s *windowsService) validateSelectionDesktop() error {
	if s.frame == nil { return nil }
	desktop, err := readWindowsDesktop()
	if err != nil { return err }
	if desktop.x != s.frame.x || desktop.y != s.frame.y || desktop.width != s.frame.width || desktop.height != s.frame.height || desktop.topology != s.frame.topology {
		return fmt.Errorf("Дисплеи или масштаб изменились; повтори выделение области")
	}
	return nil
}

func (s *windowsService) selectionPoint(window uintptr) (image.Point, error) {
	var point winPoint
	if result, _, err := user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&point))); result == 0 {
		return image.Point{}, fmt.Errorf("Не удалось определить положение указателя: %w", err)
	}
	if result, _, err := user32.NewProc("ScreenToClient").Call(window, uintptr(unsafe.Pointer(&point))); result == 0 {
		return image.Point{}, fmt.Errorf("Не удалось перевести координаты выделения: %w", err)
	}
	return image.Pt(int(point.X), int(point.Y)), nil
}
