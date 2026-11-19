//go:build linux

package platform

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"math/bits"
	"strconv"
	"strings"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"skrinpolka/internal/core"
)

func keySymbol(shortcut Shortcut) xproto.Keysym {
	if shortcut.Key == "PrintScreen" { return 0xff61 }
	if strings.HasPrefix(shortcut.Key, "F") && len(shortcut.Key) > 1 {
		n, _ := strconv.Atoi(shortcut.Key[1:]); return xproto.Keysym(0xffbd + n)
	}
	return xproto.Keysym(strings.ToLower(shortcut.Key)[0])
}

func findKeycode(c *xgb.Conn, symbol xproto.Keysym) (xproto.Keycode, error) {
	setup := xproto.Setup(c)
	reply, err := xproto.GetKeyboardMapping(c, setup.MinKeycode, byte(int(setup.MaxKeycode)-int(setup.MinKeycode)+1)).Reply()
	if err != nil { return 0, err }
	for index, candidate := range reply.Keysyms {
		if candidate == symbol { return setup.MinKeycode + xproto.Keycode(index/int(reply.KeysymsPerKeycode)), nil }
	}
	return 0, fmt.Errorf("Клавиша отсутствует в текущей раскладке X11")
}

func registerX11Shortcut(shortcut Shortcut, callback func()) (func(), error) {
	c, err := xgb.NewConn()
	if err != nil { return nil, err }
	key, err := findKeycode(c, keySymbol(shortcut))
	if err != nil { c.Close(); return nil, err }
	root := xproto.Setup(c).DefaultScreen(c).Root
	var mask uint16
	if shortcut.Shift { mask |= xproto.ModMaskShift }
	if shortcut.Ctrl { mask |= xproto.ModMaskControl }
	if shortcut.Alt { mask |= xproto.ModMask1 }
	if shortcut.Super { mask |= xproto.ModMask4 }
	// CapsLock and the usual NumLock modifier do not disable the shortcut.
	for _, extra := range []uint16{0, xproto.ModMaskLock, xproto.ModMask2, xproto.ModMaskLock | xproto.ModMask2} {
		if err := xproto.GrabKeyChecked(c, false, root, mask|extra, key, xproto.GrabModeAsync, xproto.GrabModeAsync).Check(); err != nil {
			c.Close(); return nil, fmt.Errorf("Комбинация занята или недоступна в X11: %w", err)
		}
	}
	var once sync.Once
	stop := func() { once.Do(c.Close) }
	go func() {
		defer stop()
		pressed, released := false, false
		var releasedAt xproto.Timestamp
		for {
			event, err := c.WaitForEvent()
			if err != nil || event == nil { return }
			switch value := event.(type) {
			case xproto.KeyReleaseEvent:
				if value.Detail == key { pressed = false; released = true; releasedAt = value.Time }
			case xproto.KeyPressEvent:
				if value.Detail != key { continue }
				// Ignore repeat presses, including the legacy X11 synthetic
				// release/press pair with a shared timestamp. Holding the quiet
				// shortcut must not keep saving new screenshots.
				repeat := pressed || released && releasedAt == value.Time
				pressed = true
				if !repeat && callback != nil { go callback() }
			}
		}
	}()
	return stop, nil
}

func captureX11(ctx context.Context, fullScreen bool) (Capture, error) {
	return captureX11Region(ctx,fullScreen,nil)
}

func captureX11Region(ctx context.Context, fullScreen bool, fixed *core.ScreenRegion) (Capture, error) {
	c, err := xgb.NewConn()
	if err != nil { return Capture{}, fmt.Errorf("Не удалось открыть X11: %w", err) }
	defer c.Close()
	done := make(chan struct{}); defer close(done)
	go func() { select { case <-ctx.Done(): c.Close(); case <-done: } }()
	setup := xproto.Setup(c)
	screen := setup.DefaultScreen(c)
	width, height := int(screen.WidthInPixels), int(screen.HeightInPixels)
	if width <= 0 || height <= 0 || int64(width)*int64(height) > MaxCapturePixels { return Capture{}, fmt.Errorf("Рабочий стол превышает лимит захвата 32 млн пикселей") }
	if fixed!=nil{
		if err:=fixed.Validate();err!=nil{return Capture{},err}
		if !fixed.MatchesDesktop(0,0,width,height){return Capture{},fmt.Errorf("Рабочий стол изменился. Заново задай область тихого снимка в настройках")}
	}
	if fullScreen && fixed==nil && int64(width)*int64(height) > 16_000_000 { return Capture{}, fmt.Errorf("Полный снимок превышает 16 млн пикселей; используй захват области") }
	snapshot, err := xproto.GetImage(c, xproto.ImageFormatZPixmap, xproto.Drawable(screen.Root), 0, 0, screen.WidthInPixels, screen.HeightInPixels, 0xffffffff).Reply()
	if err != nil { return Capture{}, err }
	var format xproto.Format
	for _, candidate := range setup.PixmapFormats { if candidate.Depth == snapshot.Depth { format = candidate; break } }
	if format.BitsPerPixel != 16 && format.BitsPerPixel != 24 && format.BitsPerPixel != 32 { return Capture{}, fmt.Errorf("Формат пикселей X11 пока не поддержан") }
	var visual xproto.VisualInfo
	for _, depth := range screen.AllowedDepths { for _, candidate := range depth.Visuals { if candidate.VisualId == screen.RootVisual { visual = candidate } } }
	if visual.RedMask == 0 || visual.GreenMask == 0 || visual.BlueMask == 0 { return Capture{}, fmt.Errorf("Нужен TrueColor visual X11") }
	pad := int(format.ScanlinePad)
	if pad == 0 { return Capture{}, fmt.Errorf("Некорректный формат строк X11") }
	stride := ((width*int(format.BitsPerPixel) + pad-1)/pad)*pad/8
	if stride*height > len(snapshot.Data) { return Capture{}, fmt.Errorf("X11 вернул неполный снимок") }
	encode := func(region image.Rectangle) (Capture, error) {
		capture,err:=encodeX11Region(ctx, snapshot.Data, stride, int(format.BitsPerPixel)/8, setup.ImageByteOrder, visual, region)
		if err==nil{capture.Region=&core.ScreenRegion{X:region.Min.X,Y:region.Min.Y,Width:region.Dx(),Height:region.Dy(),DesktopWidth:width,DesktopHeight:height}}
		return capture,err
	}
	if fixed!=nil{return encode(image.Rect(fixed.X,fixed.Y,fixed.X+fixed.Width,fixed.Y+fixed.Height))}
	if fullScreen { return encode(image.Rect(0, 0, width, height)) }
	cursor, err := newX11Crosshair(c)
	if err != nil { return Capture{}, fmt.Errorf("Не удалось создать крестик X11: %w", err) }
	defer xproto.FreeCursor(c, cursor)
	window, err := xproto.NewWindowId(c)
	if err != nil { return Capture{}, err }
	pixmap, err := xproto.NewPixmapId(c)
	if err != nil { return Capture{}, err }
	gc, err := xproto.NewGcontextId(c)
	if err != nil { return Capture{}, err }
	if err := xproto.CreatePixmapChecked(c, screen.RootDepth, pixmap, xproto.Drawable(screen.Root), screen.WidthInPixels, screen.HeightInPixels).Check(); err != nil { return Capture{}, err }
	defer xproto.FreePixmap(c, pixmap)
	if err := xproto.CreateGCChecked(c, gc, xproto.Drawable(pixmap), 0, nil).Check(); err != nil { return Capture{}, err }
	defer xproto.FreeGC(c, gc)
	rows := max(1, (int(setup.MaximumRequestLength)*4-64)/stride)
	for y := 0; y < height; y += rows {
		count := min(rows, height-y)
		if err := xproto.PutImageChecked(c, xproto.ImageFormatZPixmap, xproto.Drawable(pixmap), gc, uint16(width), uint16(count), 0, int16(y), 0, screen.RootDepth, snapshot.Data[y*stride:(y+count)*stride]).Check(); err != nil { return Capture{}, err }
	}
	mask := uint32(xproto.CwBackPixmap | xproto.CwOverrideRedirect | xproto.CwEventMask | xproto.CwCursor)
	events := uint32(xproto.EventMaskButtonPress | xproto.EventMaskButtonRelease | xproto.EventMaskPointerMotion | xproto.EventMaskKeyPress | xproto.EventMaskExposure)
	if err := xproto.CreateWindowChecked(c, screen.RootDepth, window, screen.Root, 0, 0, screen.WidthInPixels, screen.HeightInPixels, 0, xproto.WindowClassInputOutput, screen.RootVisual, mask, []uint32{uint32(pixmap), 1, events, uint32(cursor)}).Check(); err != nil { return Capture{}, err }
	defer xproto.DestroyWindow(c, window)
	xproto.MapWindow(c, window)
	grab, err := xproto.GrabPointer(c, false, window, uint16(xproto.EventMaskButtonPress|xproto.EventMaskButtonRelease|xproto.EventMaskPointerMotion), xproto.GrabModeAsync, xproto.GrabModeAsync, window, cursor, xproto.TimeCurrentTime).Reply()
	if err != nil || grab.Status != xproto.GrabStatusSuccess { return Capture{}, fmt.Errorf("Не удалось начать выделение X11") }
	defer xproto.UngrabPointer(c, xproto.TimeCurrentTime)
	keyboard, err := xproto.GrabKeyboard(c, false, window, xproto.TimeCurrentTime, xproto.GrabModeAsync, xproto.GrabModeAsync).Reply()
	if err != nil || keyboard.Status != xproto.GrabStatusSuccess { return Capture{}, fmt.Errorf("Не удалось подключить Escape для отмены") }
	defer xproto.UngrabKeyboard(c, xproto.TimeCurrentTime)
	escape, err := findKeycode(c, 0xff1b)
	if err != nil { return Capture{}, err }
	xproto.ChangeGC(c, gc, xproto.GcFunction|xproto.GcForeground|xproto.GcLineWidth, []uint32{6, screen.WhitePixel ^ screen.BlackPixel, 2})
	var start image.Point
	var region image.Rectangle
	pressed, drawn := false, false
	draw := func(rect image.Rectangle) {
		if rect.Dx() > 0 && rect.Dy() > 0 { xproto.PolyRectangle(c, xproto.Drawable(window), gc, []xproto.Rectangle{{X:int16(rect.Min.X), Y:int16(rect.Min.Y), Width:uint16(rect.Dx()-1), Height:uint16(rect.Dy()-1)}}) }
	}
	for {
		event, err := c.WaitForEvent()
		if err != nil || event == nil {
			if ctx.Err() != nil { return Capture{}, ErrCancelled }
			return Capture{}, fmt.Errorf("Выделение X11 прервано")
		}
		switch value := event.(type) {
		case xproto.KeyPressEvent:
			if value.Detail == escape { return Capture{}, ErrCancelled }
		case xproto.ButtonPressEvent:
			if value.Detail != 1 { return Capture{}, ErrCancelled }
			start = image.Pt(int(value.EventX), int(value.EventY)); pressed = true
		case xproto.MotionNotifyEvent:
			if !pressed { continue }
			if drawn { draw(region) }
			region = image.Rect(start.X, start.Y, int(value.EventX), int(value.EventY)).Intersect(image.Rect(0,0,width,height))
			draw(region); drawn = true
		case xproto.ExposeEvent:
			if value.Count == 0 && pressed { draw(region) }
		case xproto.ButtonReleaseEvent:
			if !pressed { continue }
			region = image.Rect(start.X, start.Y, int(value.EventX), int(value.EventY)).Intersect(image.Rect(0,0,width,height))
			if region.Empty() { return Capture{}, ErrCancelled }
			if int64(region.Dx())*int64(region.Dy()) > 16_000_000 { return Capture{}, fmt.Errorf("Выбери область до 16 млн пикселей") }
			return encode(region)
		}
	}
}

func newX11Crosshair(c *xgb.Conn) (xproto.Cursor, error) {
	font, err := xproto.NewFontId(c)
	if err != nil { return 0, err }
	if err := xproto.OpenFontChecked(c, font, 6, "cursor").Check(); err != nil { return 0, err }
	defer xproto.CloseFont(c, font)
	cursor, err := xproto.NewCursorId(c)
	if err != nil { return 0, err }
	// XC_crosshair and its mask in the standard X11 cursor font.
	if err := xproto.CreateGlyphCursorChecked(c, cursor, font, font, 34, 35, 0, 0, 0, 65535, 65535, 65535).Check(); err != nil { return 0, err }
	return cursor, nil
}

func encodeX11Region(ctx context.Context, data []byte, stride, pixelBytes int, order byte, visual xproto.VisualInfo, region image.Rectangle) (Capture, error) {
	output := image.NewNRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	channel := func(pixel, mask uint32) uint8 {
		shift := bits.TrailingZeros32(mask)
		return uint8(((pixel&mask)>>shift)*255/(mask>>shift))
	}
	for y := 0; y < region.Dy(); y++ {
		if ctx.Err() != nil { return Capture{}, ErrCancelled }
		for x := 0; x < region.Dx(); x++ {
			at := (region.Min.Y+y)*stride + (region.Min.X+x)*pixelBytes
			var pixel uint32
			if order == xproto.ImageOrderLSBFirst {
				switch pixelBytes {
				case 4: pixel = binary.LittleEndian.Uint32(data[at:])
				case 3: pixel = uint32(data[at]) | uint32(data[at+1])<<8 | uint32(data[at+2])<<16
				case 2: pixel = uint32(binary.LittleEndian.Uint16(data[at:]))
				}
			} else {
				for index := 0; index < pixelBytes; index++ { pixel = pixel<<8 | uint32(data[at+index]) }
			}
			i := y*output.Stride + x*4
			output.Pix[i], output.Pix[i+1], output.Pix[i+2], output.Pix[i+3] = channel(pixel, visual.RedMask), channel(pixel, visual.GreenMask), channel(pixel, visual.BlueMask), 255
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, output); err != nil { return Capture{}, err }
	return Capture{PNG: encoded.Bytes(), MonitorID: "x11-root", Scale: 1}, nil
}
