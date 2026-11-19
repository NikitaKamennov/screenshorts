//go:build linux

package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const portalName = "org.freedesktop.portal.Desktop"
const portalPath = dbus.ObjectPath("/org/freedesktop/portal/desktop")

func (s *linuxService) portalRequest(ctx context.Context, iface, method string, args []interface{}, options map[string]dbus.Variant) (map[string]dbus.Variant, error) {
	var token [12]byte
	if _, err := rand.Read(token[:]); err != nil { return nil, err }
	options["handle_token"] = dbus.MakeVariant("sk" + hex.EncodeToString(token[:]))
	channel := make(chan *dbus.Signal, 16)
	s.bus.Signal(channel)
	defer s.bus.RemoveSignal(channel)
	match := []dbus.MatchOption{dbus.WithMatchSender(portalName), dbus.WithMatchInterface("org.freedesktop.portal.Request"), dbus.WithMatchMember("Response")}
	if err := s.bus.AddMatchSignal(match...); err != nil { return nil, err }
	defer s.bus.RemoveMatchSignal(match...)
	var handle dbus.ObjectPath
	arguments := append(append([]interface{}{}, args...), options)
	if err := s.bus.Object(portalName, portalPath).CallWithContext(ctx, iface+"."+method, 0, arguments...).Store(&handle); err != nil { return nil, err }
	for {
		select {
		case <-ctx.Done():
			closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			s.bus.Object(portalName, handle).CallWithContext(closeCtx, "org.freedesktop.portal.Request.Close", 0)
			cancel()
			return nil, ctx.Err()
		case signal, open := <-channel:
			if !open { return nil, fmt.Errorf("Соединение с portal закрыто") }
			if signal == nil || signal.Path != handle { continue }
			var code uint32
			var result map[string]dbus.Variant
			if err := dbus.Store(signal.Body, &code, &result); err != nil { return nil, err }
			if code == 1 { return nil, ErrCancelled }
			if code != 0 { return nil, fmt.Errorf("Portal отказал в операции (код %d)", code) }
			return result, nil
		}
	}
}

func (s *linuxService) capturePortal(ctx context.Context, fullScreen bool) (Capture, error) {
	options := map[string]dbus.Variant{"interactive": dbus.MakeVariant(!fullScreen), "modal": dbus.MakeVariant(true)}
	if fullScreen {
		var version dbus.Variant
		object := s.bus.Object(portalName, portalPath)
		if err := object.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.portal.Screenshot", "version").Store(&version); err == nil {
			if number, ok := version.Value().(uint32); ok && number >= 3 {
				var targets dbus.Variant
				if err := object.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.portal.Screenshot", "AvailableTargets").Store(&targets); err != nil { return Capture{}, err }
				mask, ok := targets.Value().(uint32)
				if !ok || mask&1 == 0 { return Capture{}, fmt.Errorf("Portal не поддерживает снимок всего экрана") }
				options["target"] = dbus.MakeVariant(uint32(1))
			}
		}
	}
	result, err := s.portalRequest(ctx, "org.freedesktop.portal.Screenshot", "Screenshot", []interface{}{""}, options)
	if err != nil { return Capture{}, err }
	raw, ok := result["uri"].Value().(string)
	if !ok { return Capture{}, fmt.Errorf("Portal не вернул снимок") }
	uri, err := url.Parse(raw)
	if err != nil || uri.Scheme != "file" || uri.Host != "" && uri.Host != "localhost" { return Capture{}, fmt.Errorf("Portal вернул неподдержанный адрес снимка") }
	file, err := os.Open(uri.Path)
	if err != nil { return Capture{}, err }
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() { return Capture{}, fmt.Errorf("Снимок portal недоступен") }
	const maxBytes = 32 << 20
	if info.Size() > maxBytes { return Capture{}, fmt.Errorf("Снимок portal превышает 32 MiB; выбери меньшую область") }
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil || len(data) > maxBytes { return Capture{}, fmt.Errorf("Не удалось прочитать снимок portal в пределах лимита") }
	// The URI belongs to the portal, not the application's library. Do not
	// remove arbitrary system-managed files merely because a URI was supplied.
	return Capture{PNG: data, MonitorID: "portal", Scale: 1}, nil
}

type portalShortcut struct { ID string; Properties map[string]dbus.Variant }

func (s *linuxService) bindPortalShortcuts(ctx context.Context, desired Shortcuts) (Shortcuts, dbus.ObjectPath, func(), error) {
	failure := func(err error) (Shortcuts, dbus.ObjectPath, func(), error) { return Shortcuts{}, "", nil, err }
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute); defer cancel()
	var token [8]byte
	if _, err := rand.Read(token[:]); err != nil { return failure(err) }
	result, err := s.portalRequest(ctx, "org.freedesktop.portal.GlobalShortcuts", "CreateSession", nil, map[string]dbus.Variant{
		"session_handle_token": dbus.MakeVariant("sk" + hex.EncodeToString(token[:])),
	})
	if err != nil { return failure(err) }
	raw, ok := result["session_handle"].Value().(string)
	if !ok || !dbus.ObjectPath(raw).IsValid() { return failure(fmt.Errorf("Portal не создал сессию клавиш")) }
	session := dbus.ObjectPath(raw)
	success := false
	defer func() { if !success { s.closePortalSession(session) } }()
	shortcuts := []portalShortcut{}
	for _, binding := range []struct{ id, description, trigger string }{
		{"capture", "ScreenShorts Pro: выделить область экрана", desired.Area},
		{"capture-screen", "ScreenShorts Pro: снимок всего экрана", desired.Screen},
	} {
		if binding.trigger == "" { continue }
		trigger := strings.ReplaceAll(strings.ToUpper(binding.trigger), "PRINTSCREEN", "Print")
		shortcuts = append(shortcuts, portalShortcut{binding.id, map[string]dbus.Variant{
			"description": dbus.MakeVariant(binding.description), "preferred_trigger": dbus.MakeVariant(trigger),
		}})
	}
	bound, err := s.portalRequest(ctx, "org.freedesktop.portal.GlobalShortcuts", "BindShortcuts", []interface{}{session, shortcuts, ""}, map[string]dbus.Variant{})
	if err != nil { return failure(err) }
	var registered []portalShortcut
	if err := dbus.Store([]interface{}{bound["shortcuts"].Value()}, &registered); err != nil { return failure(err) }
	actual := Shortcuts{}
	for _, item := range registered {
		if item.ID == "capture" && desired.Area != "" { actual.Area, _ = item.Properties["trigger_description"].Value().(string) }
		if item.ID == "capture-screen" && desired.Screen != "" { actual.Screen, _ = item.Properties["trigger_description"].Value().(string) }
	}
	if desired.Area != "" && actual.Area == "" || desired.Screen != "" && actual.Screen == "" {
		return failure(fmt.Errorf("Системный диалог назначил не все запрошенные комбинации"))
	}
	if desired.Area != "" && desired.Screen != "" && actual.Area == actual.Screen {
		return failure(fmt.Errorf("Системный диалог назначил одинаковые комбинации для двух действий"))
	}
	channel := make(chan *dbus.Signal, 16)
	s.bus.Signal(channel)
	match := []dbus.MatchOption{dbus.WithMatchSender(portalName), dbus.WithMatchInterface("org.freedesktop.portal.GlobalShortcuts"), dbus.WithMatchMember("Activated")}
	if err := s.bus.AddMatchSignal(match...); err != nil { s.bus.RemoveSignal(channel); return failure(err) }
	var once sync.Once
	done := make(chan struct{})
	stop := func() { once.Do(func() { s.bus.RemoveMatchSignal(match...); s.bus.RemoveSignal(channel); close(done) }) }
	go func() {
		for {
			select {
			case <-done: return
			case signal, open := <-channel:
				if !open { return }
				if signal == nil || len(signal.Body) < 2 { continue }
				path, _ := signal.Body[0].(dbus.ObjectPath)
				id, _ := signal.Body[1].(string)
				s.mu.Lock()
				active := !s.closed && s.session == session && path == session
				s.mu.Unlock()
				if !active { continue }
				if id == "capture" && desired.Area != "" && s.actions.Capture != nil { go s.actions.Capture() }
				if id == "capture-screen" && desired.Screen != "" && s.actions.CaptureScreen != nil { go s.actions.CaptureScreen() }
			}
		}
	}()
	success = true
	return actual, session, stop, nil
}
