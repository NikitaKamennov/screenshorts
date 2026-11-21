//go:build windows

package platform

import (
	"context"
	"fmt"
	"image"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

type windowsCommand struct {
	ctx context.Context
	action func() error
	reply chan error
	state atomic.Uint32 // queued=0, executing=1, cancelled before execution=2
}

func (s *windowsService) requestStop() { s.stopOnce.Do(func() { close(s.stop) }) }

func (s *windowsService) stopRequested() bool {
	select { case <-s.stop: return true; default: return false }
}

func (s *windowsService) loop(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(s.done)
	var loopErr error
	defer func() { s.releaseNative(loopErr) }()
	if loopErr = s.initializeNative(); loopErr != nil {
		ready <- loopErr
		return
	}
	s.commandMu.Lock()
	s.accepting = true
	s.commandMu.Unlock()
	ready <- nil
	for !s.stopRequested() {
		var message winMessage
		result, _, err := user32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) == -1 {
			loopErr = fmt.Errorf("Остановлена очередь сообщений Windows: %w", err)
			return
		}
		if result == 0 || s.stopRequested() { return }
		user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&message)))
		user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&message)))
	}
}

func (s *windowsService) initializeNative() error {
	if processDPIError != nil { return processDPIError }
	dpi := user32.NewProc("SetThreadDpiAwarenessContext")
	if err := dpi.Find(); err != nil { return fmt.Errorf("Не удалось настроить DPI потока захвата: %w", err) }
	s.previousDPI, _, _ = dpi.Call(perMonitorV2())
	if s.previousDPI == 0 || !threadUsesPerMonitorV2() { return fmt.Errorf("Windows не подтвердила per-monitor DPI v2 для потока захвата") }
	s.instance, _, _ = kernel32.NewProc("GetModuleHandleW").Call(0)
	s.className, _ = syscall.UTF16PtrFromString("SkrinpolkaNativeEvents")
	s.crosshair, _, _ = user32.NewProc("LoadCursorW").Call(0, 32515)
	if s.instance == 0 || s.crosshair == 0 { return fmt.Errorf("Не удалось подготовить ресурсы окна захвата Windows") }
	class := winClass{Procedure:nativeWindowProc, Instance:s.instance, Cursor:s.crosshair, ClassName:s.className}
	class.Size = uint32(unsafe.Sizeof(class))
	if result, _, err := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class))); result == 0 {
		return fmt.Errorf("Не удалось зарегистрировать окно захвата: %w", err)
	}
	s.classRegistered = true
	window, _, err := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(s.className)), 0, 0, 0, 0, 0, 0, 0, 0, s.instance, 0)
	if window == 0 { return fmt.Errorf("Не удалось создать native-сервис: %w", err) }
	s.commandMu.Lock()
	s.window = window
	s.commandMu.Unlock()
	windowsByHandle.Store(window, s)
	// The timer wakes GetMessage even if posting the shutdown message fails. It
	// also cancels a selection whose caller stopped waiting during startup.
	s.timer, _, err = user32.NewProc("SetTimer").Call(window, 1, 250, 0)
	if s.timer == 0 { return fmt.Errorf("Не удалось подготовить остановку native-сервиса: %w", err) }
	icon, _, _ := user32.NewProc("LoadIconW").Call(0, 32512)
	s.icon = notifyIcon{Window:window, ID:1, Flags:1|2|4, Callback:wmTray, Icon:icon}
	s.icon.Size = uint32(unsafe.Sizeof(s.icon))
	copy(s.icon.Tip[:], syscall.StringToUTF16("ScreenShooter Pro — новый снимок / открыть библиотеку"))
	name, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	message, _, _ := user32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(name)))
	s.taskbarMessage = uint32(message)
	s.addTray()
	return nil
}

// Queue insertion and posting share a lock with dequeue. A failed PostMessage
// removes its command before the native thread can execute it later.
func (s *windowsService) onThread(ctx context.Context, action func() error) error {
	if err := ctx.Err(); err != nil { return err }
	command := &windowsCommand{ctx:ctx, action:action, reply:make(chan error, 1)}
	s.commandMu.Lock()
	if !s.accepting || s.stopRequested() {
		s.commandMu.Unlock()
		return ErrUnavailable
	}
	if len(s.commands) >= 32 {
		s.commandMu.Unlock()
		return fmt.Errorf("Очередь команд Windows занята; повтори действие после завершения текущего")
	}
	s.commands = append(s.commands, command)
	posted, _, postErr := user32.NewProc("PostMessageW").Call(s.window, wmNativeCommand, 0, 0)
	if posted == 0 {
		s.commands[len(s.commands)-1] = nil
		s.commands = s.commands[:len(s.commands)-1]
		s.commandMu.Unlock()
		return fmt.Errorf("Не удалось передать действие native-сервису: %w", postErr)
	}
	s.commandMu.Unlock()
	select {
	case err := <-command.reply: return err
	case <-ctx.Done():
		if command.state.CompareAndSwap(0, 2) { return ctx.Err() }
		// Once execution started, observe its actual result. In particular, a
		// committed hotkey change must not be reported as an unapplied cancel.
		select { case err := <-command.reply: return err; case <-s.done: return ErrUnavailable }
	case <-s.done: return ErrUnavailable
	}
}

func (s *windowsService) runCommands() {
	s.commandMu.Lock()
	commands := s.commands
	s.commands = nil
	s.commandMu.Unlock()
	for _, command := range commands {
		if s.stopRequested() { command.reply <- ErrUnavailable
		} else if err := command.ctx.Err(); err != nil { command.reply <- err
		} else if !command.state.CompareAndSwap(0, 1) { command.reply <- command.ctx.Err()
		} else { command.reply <- command.action() }
	}
}

func (s *windowsService) releaseNative(loopErr error) {
	s.requestStop()
	s.commandMu.Lock()
	s.accepting = false
	commands := s.commands
	s.commands = nil
	s.commandMu.Unlock()
	for _, command := range commands { command.reply <- ErrUnavailable }
	if s.frame != nil { s.finishCapture(image.Rectangle{}, ErrCancelled) }
	for _, id := range s.hotkeys { user32.NewProc("UnregisterHotKey").Call(s.window, uintptr(id)) }
	s.hotkeys = nil
	if s.icon.Window != 0 { shell32.NewProc("Shell_NotifyIconW").Call(2, uintptr(unsafe.Pointer(&s.icon))) }
	if s.window != 0 {
		if s.timer != 0 { user32.NewProc("KillTimer").Call(s.window, s.timer) }
		user32.NewProc("DestroyWindow").Call(s.window)
		windowsByHandle.Delete(s.window)
		s.commandMu.Lock()
		s.window = 0
		s.commandMu.Unlock()
	}
	if s.classRegistered { user32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(s.className)), s.instance) }
	if s.previousDPI != 0 { user32.NewProc("SetThreadDpiAwarenessContext").Call(s.previousDPI) }
	reason := "Системный сервис Windows остановлен"
	if loopErr != nil { reason = loopErr.Error() }
	unavailable := Feature{Reason:reason}
	s.mu.Lock()
	s.loopError = loopErr
	s.caps.Tray, s.caps.AreaCapture, s.caps.ScreenCapture = unavailable, unavailable, unavailable
	s.caps.SilentCapture = unavailable
	s.caps.GlobalShortcut, s.caps.GlobalScreenShortcut = unavailable, unavailable
	s.caps.GlobalSilentShortcut, s.caps.GlobalSilentAreaShortcut = unavailable, unavailable
	s.caps.Shortcut, s.caps.ScreenShortcut, s.caps.SilentShortcut, s.caps.SilentAreaShortcut = "", "", "", ""
	s.mu.Unlock()
	s.changed()
}

func (s *windowsService) Close() error {
	s.requestStop()
	s.commandMu.Lock()
	if s.window != 0 { user32.NewProc("PostMessageW").Call(s.window, wmClose, 0, 0) }
	s.commandMu.Unlock()
	timer := time.NewTimer(5*time.Second)
	defer timer.Stop()
	select {
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.loopError
	case <-timer.C:
		return fmt.Errorf("Native-сервис Windows не подтвердил остановку за 5 секунд")
	}
}
