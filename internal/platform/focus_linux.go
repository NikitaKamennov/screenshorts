//go:build linux

package platform

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

type x11Focus struct { window xproto.Window; pid uint32 }

func focusConnection(ctx context.Context) (*xgb.Conn, func(), error) {
	c, err := xgb.NewConn()
	if err != nil { return nil, nil, err }
	stop := context.AfterFunc(ctx, c.Close)
	return c, func(){ stop(); c.Close() }, nil
}

func focusAtom(c *xgb.Conn, name string) (xproto.Atom, error) {
	reply, err := xproto.InternAtom(c, false, uint16(len(name)), name).Reply()
	if err != nil { return 0, err }; if reply == nil { return 0, fmt.Errorf("Нет свойства окна") }
	return reply.Atom, nil
}

func windowPID(c *xgb.Conn, window xproto.Window) uint32 {
	atom, err := focusAtom(c, "_NET_WM_PID"); if err != nil { return 0 }
	value, err := xproto.GetProperty(c, false, window, atom, xproto.AtomCardinal, 0, 1).Reply()
	if err != nil || value == nil || value.Format != 32 || len(value.Value) < 4 { return 0 }
	return xgb.Get32(value.Value)
}

func RememberFocus(ctx context.Context) FocusTarget {
	// XWayland cannot identify the compositor's native active Wayland window.
	if os.Getenv("XDG_SESSION_TYPE") != "x11" { return nil }
	ctx, cancel := context.WithTimeout(ctx, time.Second); defer cancel()
	c, closeConnection, err := focusConnection(ctx); if err != nil { return nil }; defer closeConnection()
	atom, err := focusAtom(c, "_NET_ACTIVE_WINDOW"); if err != nil { return nil }
	root := xproto.Setup(c).DefaultScreen(c).Root
	value, err := xproto.GetProperty(c, false, root, atom, xproto.AtomWindow, 0, 1).Reply()
	if err != nil || value == nil || value.Format != 32 || len(value.Value) < 4 { return nil }
	window := xproto.Window(xgb.Get32(value.Value))
	if window == 0 || window == root { return nil }
	pid := windowPID(c, window)
	if pid == uint32(os.Getpid()) { return nil }
	return x11Focus{window:window, pid:pid}
}

func (target x11Focus) Restore(ctx context.Context) error {
	c, closeConnection, err := focusConnection(ctx); if err != nil { return err }; defer closeConnection()
	if _, err := xproto.GetWindowAttributes(c, target.window).Reply(); err != nil { return fmt.Errorf("Прежнее окно уже закрыто") }
	if target.pid != 0 && windowPID(c, target.window) != target.pid { return fmt.Errorf("Прежнее окно изменилось") }
	atom, err := focusAtom(c, "_NET_ACTIVE_WINDOW"); if err != nil { return err }
	event := xproto.ClientMessageEvent{Format:32, Window:target.window, Type:atom,
		Data:xproto.ClientMessageDataUnionData32New([]uint32{1, 0, 0, 0, 0})}
	root := xproto.Setup(c).DefaultScreen(c).Root
	return xproto.SendEventChecked(c, false, root, xproto.EventMaskSubstructureRedirect|xproto.EventMaskSubstructureNotify, string(event.Bytes())).Check()
}
