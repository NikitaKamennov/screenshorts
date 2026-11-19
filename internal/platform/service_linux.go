//go:build linux

package platform

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"skrinpolka/internal/core"
)

type linuxService struct {
	mu sync.Mutex
	shortcutMu sync.Mutex
	actions Actions
	caps Capabilities
	bus *dbus.Conn
	stopShortcut func()
	session dbus.ObjectPath
	closed bool
	preferred Shortcuts
	bindings map[string]func()
}

func New(actions Actions) (Service, error) {
	s := &linuxService{actions: actions, caps: Detect(), bindings: map[string]func(){}}
	s.caps.AreaCapture = Feature{Available: true, Reason: "Системный выбор области"}
	s.caps.ScreenCapture = Feature{Available: true, Reason: "Снимок всего экрана"}
	if s.caps.Session == "x11" {
		s.caps.AreaCapture.Reason = "Выделение области X11; Escape — отмена"
		s.caps.SilentCapture = Feature{Available:true, Reason:"Полный снимок X11 без окна выделения и активации приложения"}
	} else {
		s.caps.SilentCapture = Feature{Reason:"Тихий снимок недоступен в Wayland: Screenshot portal может открывать системный диалог"}
		s.caps.GlobalSilentShortcut = s.caps.SilentCapture
		s.caps.GlobalSilentAreaShortcut = s.caps.SilentCapture
		s.caps.Session = "wayland/portal"
		s.caps.AreaCapture.Reason = "Системный Screenshot portal; выбор зависит от окружения"
		s.caps.ScreenCapture.Reason = "Полный снимок через Screenshot portal; разрешение зависит от окружения"
	}
	bus, err := dbus.ConnectSessionBus()
	if err == nil {
		s.bus = bus
		err = s.startTray()
	}
	if err != nil { s.caps.Tray = Feature{Reason: "Трей недоступен: " + err.Error()} }
	if s.bus == nil && s.caps.Session != "x11" {
		s.caps.AreaCapture = Feature{Reason: "Нет соединения с системным portal"}
		s.caps.ScreenCapture = s.caps.AreaCapture
	}
	return s, nil
}

func (s *linuxService) Capabilities() Capabilities {
	s.mu.Lock(); defer s.mu.Unlock()
	return s.caps
}

func (s *linuxService) changed() {
	if s.actions.Changed != nil { s.actions.Changed(s.Capabilities()) }
}

func (s *linuxService) SetShortcuts(ctx context.Context, requested Shortcuts) (Shortcuts, error) {
	desired, err := NormalizeShortcuts(requested)
	if err != nil { return Shortcuts{}, err }
	if (desired.Silent != "" || desired.SilentArea != "") && s.caps.Session != "x11" { return Shortcuts{}, fmt.Errorf("%s", s.caps.SilentCapture.Reason) }
	s.shortcutMu.Lock()
	defer s.shortcutMu.Unlock()
	s.mu.Lock()
	if s.closed { s.mu.Unlock(); return Shortcuts{}, ErrUnavailable }
	actual := Shortcuts{Area: s.caps.Shortcut, Screen: s.caps.ScreenShortcut, Silent: s.caps.SilentShortcut, SilentArea:s.caps.SilentAreaShortcut}
	ready := (desired.Area == "" || s.caps.GlobalShortcut.Available) && (desired.Screen == "" || s.caps.GlobalScreenShortcut.Available) && (desired.Silent == "" || s.caps.GlobalSilentShortcut.Available) && (desired.SilentArea == "" || s.caps.GlobalSilentAreaShortcut.Available)
	if desired == s.preferred && ready && (desired.Area != "" || desired.Screen != "" || desired.Silent != "" || desired.SilentArea != "") { s.mu.Unlock(); return actual, nil }
	s.mu.Unlock()
	if s.caps.Session == "x11" {
		err = s.setX11Shortcuts(ctx, desired)
		actual = desired
	} else {
		actual, err = s.setPortalShortcuts(ctx, desired)
	}
	if err != nil {
		s.mu.Lock()
		if desired.Area != "" && !s.caps.GlobalShortcut.Available { s.caps.GlobalShortcut = Feature{Reason: "Область: " + err.Error()} }
		if desired.Screen != "" && !s.caps.GlobalScreenShortcut.Available { s.caps.GlobalScreenShortcut = Feature{Reason: "Весь экран: " + err.Error()} }
		if desired.Silent != "" && !s.caps.GlobalSilentShortcut.Available { s.caps.GlobalSilentShortcut = Feature{Reason: "Тихий снимок: " + err.Error()} }
		if desired.SilentArea != "" && !s.caps.GlobalSilentAreaShortcut.Available { s.caps.GlobalSilentAreaShortcut = Feature{Reason: "Тихая область: " + err.Error()} }
		s.mu.Unlock()
		s.changed()
		return Shortcuts{}, err
	}
	s.mu.Lock()
	s.preferred = desired
	s.caps.Shortcut, s.caps.ScreenShortcut = actual.Area, actual.Screen
	s.caps.SilentShortcut = actual.Silent
	s.caps.SilentAreaShortcut = actual.SilentArea
	s.caps.GlobalShortcut = shortcutFeature(actual.Area, "Комбинация области назначена")
	s.caps.GlobalScreenShortcut = shortcutFeature(actual.Screen, "Комбинация всего экрана назначена")
	if s.caps.SilentCapture.Available { s.caps.GlobalSilentShortcut = shortcutFeature(actual.Silent, "Комбинация тихого снимка назначена") }
	if s.caps.SilentCapture.Available { s.caps.GlobalSilentAreaShortcut = shortcutFeature(actual.SilentArea, "Комбинация тихой области назначена") }
	s.mu.Unlock()
	s.changed()
	return actual, nil
}

func (s *linuxService) setX11Shortcuts(ctx context.Context, desired Shortcuts) error {
	next, added := map[string]func(){}, map[string]func(){}
	committed := false
	defer func() { if !committed { for _, stop := range added { stop() } } }()
	for _, text := range []string{desired.Area, desired.Screen, desired.Silent, desired.SilentArea} {
		if text == "" { continue }
		if stop, ok := s.bindings[text]; ok { next[text] = stop; continue }
		if err := ctx.Err(); err != nil { return err }
		shortcut, _ := ParseShortcut(text)
		binding := text
		stop, err := registerX11Shortcut(shortcut, func() {
			s.mu.Lock()
			var action func()
			if !s.closed {
				if s.preferred.Area == binding { action = s.actions.Capture }
				if s.preferred.Screen == binding { action = s.actions.CaptureScreen }
				if s.preferred.Silent == binding { action = s.actions.CaptureSilent }
				if s.preferred.SilentArea == binding { action = s.actions.CaptureSilentArea }
			}
			s.mu.Unlock()
			if action != nil { action() }
		})
		if err != nil { return err }
		next[text], added[text] = stop, stop
	}
	if err := ctx.Err(); err != nil { return err }
	for text, stop := range s.bindings { if _, kept := next[text]; !kept { stop() } }
	s.bindings = next
	committed = true
	return nil
}

func (s *linuxService) setPortalShortcuts(ctx context.Context, desired Shortcuts) (Shortcuts, error) {
	if s.bus == nil {
		if desired.Area == "" && desired.Screen == "" { return Shortcuts{}, nil }
		return Shortcuts{}, ErrUnavailable
	}
	var actual Shortcuts
	var session dbus.ObjectPath
	var stop func()
	var err error
	if desired.Area != "" || desired.Screen != "" {
		actual, session, stop, err = s.bindPortalShortcuts(ctx, desired)
		if err != nil { return Shortcuts{}, err }
	}
	// Bind a new session before removing the old one. A rejected permission
	// dialog must not destroy the previously working pair of shortcuts.
	s.mu.Lock()
	oldSession, oldStop := s.session, s.stopShortcut
	s.session, s.stopShortcut = session, stop
	s.mu.Unlock()
	if oldStop != nil { oldStop() }
	s.closePortalSession(oldSession)
	return actual, nil
}

func (s *linuxService) closePortalSession(session dbus.ObjectPath) {
	if session == "" || s.bus == nil { return }
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s.bus.Object(portalName, session).CallWithContext(ctx, "org.freedesktop.portal.Session.Close", 0)
}

func (s *linuxService) CaptureArea(ctx context.Context) (Capture, error) {
	if s.caps.Session == "x11" { return captureX11(ctx, false) }
	if s.bus == nil { return Capture{}, ErrUnavailable }
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute); defer cancel()
	return s.capturePortal(ctx, false)
}

func (s *linuxService) CaptureScreen(ctx context.Context) (Capture, error) {
	if s.caps.Session == "x11" { return captureX11(ctx, true) }
	if s.bus == nil { return Capture{}, ErrUnavailable }
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute); defer cancel()
	return s.capturePortal(ctx, true)
}

func (s *linuxService) CaptureSilent(ctx context.Context) (Capture, error) {
	if s.caps.Session != "x11" { return Capture{}, fmt.Errorf("%s", s.caps.SilentCapture.Reason) }
	return captureX11(ctx, true)
}

func (s *linuxService) CaptureRegion(ctx context.Context, region core.ScreenRegion) (Capture, error) {
	if s.caps.Session != "x11" { return Capture{}, fmt.Errorf("%s", s.caps.SilentCapture.Reason) }
	return captureX11Region(ctx,false,&region)
}

func (s *linuxService) Close() error {
	s.shortcutMu.Lock()
	defer s.shortcutMu.Unlock()
	s.mu.Lock()
	if s.closed { s.mu.Unlock(); return nil }
	s.closed = true
	stop, session := s.stopShortcut, s.session
	s.stopShortcut = nil
	s.mu.Unlock()
	if stop != nil { stop() }
	for _, stop := range s.bindings { stop() }
	s.bindings = nil
	if s.bus != nil {
		s.closePortalSession(session)
		return s.bus.Close()
	}
	return nil
}

func linuxAppName() string { return fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid()) }
