//go:build windows

package platform

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
	"skrinpolka/internal/core"
)

var user32 = syscall.NewLazyDLL("user32.dll")
var shell32 = syscall.NewLazyDLL("shell32.dll")
var gdi32 = syscall.NewLazyDLL("gdi32.dll")
var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var windowsByHandle sync.Map
var nativeWindowProc = syscall.NewCallback(windowsProcedure)

const (
	wmNativeCommand = 0x8000 + 20
	wmTray = 0x8000 + 21
	wmClose = 0x10
	wmDestroy = 2
	wmPaint = 15
	wmHotkey = 0x0312
	wmTimer = 0x0113
	wmDisplayChange = 0x007e
	wmSettingChange = 0x001a
	wmDPIChanged = 0x02e0
	wmPowerBroadcast = 0x0218
)

type winPoint struct { X,Y int32 }
type winRect struct { Left,Top,Right,Bottom int32 }
type winMessage struct { Window uintptr; Message uint32; WParam,LParam uintptr; Time uint32; Point winPoint; Private uint32 }
type winClass struct {
	Size,Style uint32
	Procedure uintptr
	ClassExtra,WindowExtra int32
	Instance,Icon,Cursor,Background uintptr
	MenuName,ClassName *uint16
	SmallIcon uintptr
}
type notifyIcon struct {
	Size uint32
	Window uintptr
	ID,Flags,Callback uint32
	Icon uintptr
	Tip [128]uint16
	State,StateMask uint32
	Info [256]uint16
	Version uint32
	InfoTitle [64]uint16
	InfoFlags uint32
	GUID [16]byte
	BalloonIcon uintptr
}
type paintStruct struct { DC uintptr; Erase int32; Rect winRect; Restore,IncUpdate int32; Reserved [32]byte }
type bitmapHeader struct { Size uint32; Width,Height int32; Planes,BitCount uint16; Compression,SizeImage uint32; XPels,YPels int32; ClrUsed,ClrImportant uint32 }
type bitmapInfo struct { Header bitmapHeader; Colors [1]uint32 }

type windowsFrame struct { x,y,width,height int; topology string; dc,bitmap,previous uintptr; pixels []byte }
type captureOutcome struct { value Capture; err error }

type windowsService struct {
	mu sync.Mutex
	actions Actions
	caps Capabilities
	window uintptr
	overlay uintptr
	instance uintptr
	className *uint16
	icon notifyIcon
	taskbarMessage uint32
	commandMu sync.Mutex
	commands []*windowsCommand
	accepting bool
	stop chan struct{}
	stopOnce sync.Once
	done chan struct{}
	loopError error
	classRegistered bool
	timer uintptr
	previousDPI uintptr
	frame *windowsFrame
	result chan captureOutcome
	captureContext context.Context
	start,current image.Point
	dragging bool
	hotkeys map[string]int
	areaHotkey, screenHotkey, silentHotkey, silentAreaHotkey int
	crosshair uintptr
}

func New(actions Actions) (Service,error) {
	s:=&windowsService{actions:actions,caps:Capabilities{OS:"windows",Session:"Win32",AreaCapture:Feature{Available:true,Reason:"Нативное выделение области; Escape — отмена"},ScreenCapture:Feature{Available:true,Reason:"Весь виртуальный рабочий стол"}},stop:make(chan struct{}),done:make(chan struct{}),hotkeys:map[string]int{}}
	s.caps.SilentCapture = Feature{Available:true, Reason:"Полный снимок без активации окна"}
	ready:=make(chan error,1)
	go s.loop(ready)
	if err:=<-ready;err!=nil {<-s.done;return nil,err}
	return s,nil
}

func (s *windowsService) Capabilities() Capabilities {s.mu.Lock();defer s.mu.Unlock();return s.caps}
func (s *windowsService) changed(){if s.actions.Changed!=nil {s.actions.Changed(s.Capabilities())}}

func (s *windowsService) addTray(){
	if s.stopRequested() { return }
	if s.taskbarMessage==0 || s.icon.Icon==0 {
		s.mu.Lock();s.caps.Tray=Feature{Reason:"Трей недоступен: не подготовлены значок или восстановление Explorer"};s.mu.Unlock();s.changed();return
	}
	result,_,err:=shell32.NewProc("Shell_NotifyIconW").Call(0,uintptr(unsafe.Pointer(&s.icon)))
	s.mu.Lock();s.caps.Tray=Feature{Available:result!=0,Reason:"Системный трей Windows"};if result==0 {s.caps.Tray.Reason=fmt.Sprintf("Не удалось создать значок: %v",err)};s.mu.Unlock();s.changed()
}

func (s *windowsService) SetShortcuts(ctx context.Context, requested Shortcuts) (Shortcuts, error) {
	desired, err := NormalizeShortcuts(requested)
	if err != nil { return Shortcuts{}, err }
	err = s.onThread(ctx, func() error {
		next, added := map[string]int{}, map[string]int{}
		committed := false
		defer func() { if !committed { for _, id := range added { user32.NewProc("UnregisterHotKey").Call(s.window, uintptr(id)) } } }()
		for _, text := range []string{desired.Area, desired.Screen, desired.Silent, desired.SilentArea} {
			if text == "" { continue }
			if id, ok := s.hotkeys[text]; ok { next[text] = id; continue }
			shortcut, _ := ParseShortcut(text)
			var modifiers uint32 = 0x4000 // MOD_NOREPEAT
			if shortcut.Alt { modifiers |= 1 }; if shortcut.Ctrl { modifiers |= 2 }; if shortcut.Shift { modifiers |= 4 }; if shortcut.Super { modifiers |= 8 }
			key := uint32(0x2c)
			if shortcut.Key != "PrintScreen" {
				key = uint32(shortcut.Key[0])
				if strings.HasPrefix(shortcut.Key, "F") && len(shortcut.Key) > 1 { number, _ := strconv.Atoi(shortcut.Key[1:]); key = uint32(0x6f+number) }
			}
			id := 1
			for {
				used := false
				for _, current := range s.hotkeys { if current == id { used = true } }
				for _, current := range added { if current == id { used = true } }
				if !used { break }; id++
			}
			if result, _, err := user32.NewProc("RegisterHotKey").Call(s.window, uintptr(id), uintptr(modifiers), uintptr(key)); result == 0 {
				return fmt.Errorf("Комбинация %s занята или запрещена Windows: %w", text, err)
			}
			next[text], added[text] = id, id
		}
		// Existing registrations are reused, including a swap of the two actions.
		// A failure above leaves the entire old pair active.
		if err := ctx.Err(); err != nil { return err }
		for text, id := range s.hotkeys { if _, kept := next[text]; !kept { user32.NewProc("UnregisterHotKey").Call(s.window, uintptr(id)) } }
		s.hotkeys, s.areaHotkey, s.screenHotkey = next, next[desired.Area], next[desired.Screen]
		s.silentHotkey = next[desired.Silent]
		s.silentAreaHotkey = next[desired.SilentArea]
		committed = true
		s.mu.Lock()
		s.caps.Shortcut, s.caps.ScreenShortcut = desired.Area, desired.Screen
		s.caps.SilentShortcut = desired.Silent
		s.caps.SilentAreaShortcut = desired.SilentArea
		s.caps.GlobalShortcut = shortcutFeature(desired.Area, "RegisterHotKey: область")
		s.caps.GlobalScreenShortcut = shortcutFeature(desired.Screen, "RegisterHotKey: весь экран")
		s.caps.GlobalSilentShortcut = shortcutFeature(desired.Silent, "RegisterHotKey: тихий снимок")
		s.caps.GlobalSilentAreaShortcut = shortcutFeature(desired.SilentArea, "RegisterHotKey: тихая область")
		s.mu.Unlock()
		s.changed()
		return nil
	})
	return desired, err
}

func (s *windowsService) CaptureArea(ctx context.Context)(Capture,error){
	return s.capture(ctx, false)
}

func (s *windowsService) CaptureScreen(ctx context.Context)(Capture,error){
	return s.capture(ctx, true)
}

func (s *windowsService) CaptureSilent(ctx context.Context)(Capture,error){
	return s.capture(ctx, true)
}

func (s *windowsService) capture(ctx context.Context, fullScreen bool)(Capture,error){
	result:=make(chan captureOutcome,1)
	err:=s.onThread(ctx,func()error{return s.beginCapture(ctx, result, fullScreen)})
	if err!=nil{return Capture{},err}
	select{
	case outcome:=<-result:return outcome.value,outcome.err
	case <-ctx.Done():
		cancelCtx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel()
		s.onThread(cancelCtx,func()error{if s.result==result{s.finishCapture(image.Rectangle{},ErrCancelled)};return nil})
		return Capture{},ErrCancelled
	case <-s.done:return Capture{},ErrUnavailable
	}
}

func windowsProcedure(window uintptr,message uint32,wparam,lparam uintptr)uintptr{
	value,ok:=windowsByHandle.Load(window)
	if !ok {result,_,_:=user32.NewProc("DefWindowProcW").Call(window,uintptr(message),wparam,lparam);return result}
	s:=value.(*windowsService)
	if window==s.overlay {return s.overlayProcedure(window,message,wparam,lparam)}
	switch message{
	case wmNativeCommand:s.runCommands();return 0
	case wmTimer:
		if s.captureContext!=nil && s.captureContext.Err()!=nil { s.finishCapture(image.Rectangle{},ErrCancelled)
		} else if err:=s.validateSelectionDesktop();err!=nil { s.finishCapture(image.Rectangle{},err) };return 0
	case wmDisplayChange,wmSettingChange:
		if err:=s.validateSelectionDesktop();err!=nil{s.finishCapture(image.Rectangle{},err)};return 0
	case wmPowerBroadcast:
		if wparam==4{s.finishCapture(image.Rectangle{},ErrCancelled)};return 1 // PBT_APMSUSPEND
	case wmHotkey:
		if int(wparam)==s.areaHotkey && s.areaHotkey!=0 && s.actions.Capture!=nil{go s.actions.Capture()}
		if int(wparam)==s.screenHotkey && s.screenHotkey!=0 && s.actions.CaptureScreen!=nil{go s.actions.CaptureScreen()}
		if int(wparam)==s.silentHotkey && s.silentHotkey!=0 && s.actions.CaptureSilent!=nil{go s.actions.CaptureSilent()}
		if int(wparam)==s.silentAreaHotkey && s.silentAreaHotkey!=0 && s.actions.CaptureSilentArea!=nil{go s.actions.CaptureSilentArea()}
		return 0
	case wmTray:
		switch uint32(lparam){case 0x0203:if s.actions.OpenLibrary!=nil{go s.actions.OpenLibrary()};case 0x0205:s.popupMenu()};return 0
	case wmClose:s.requestStop();return 0
	case wmDestroy:
		s.requestStop();user32.NewProc("PostQuitMessage").Call(0);return 0
	}
	if s.taskbarMessage!=0 && message==s.taskbarMessage{s.addTray();return 0}
	result,_,_:=user32.NewProc("DefWindowProcW").Call(window,uintptr(message),wparam,lparam);return result
}

func (s *windowsService) popupMenu(){
	menu,_,_:=user32.NewProc("CreatePopupMenu").Call();if menu==0{return};defer user32.NewProc("DestroyMenu").Call(menu)
	for _, item:=range []struct{id int;label string}{{1,"Выделить область"},{5,"Весь экран"},{2,"Открыть библиотеку"},{3,"Настройки"},{4,"Выйти"}}{text,_:=syscall.UTF16PtrFromString(item.label);user32.NewProc("AppendMenuW").Call(menu,0,uintptr(item.id),uintptr(unsafe.Pointer(text)))}
	var point winPoint;user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&point)));user32.NewProc("SetForegroundWindow").Call(s.window)
	selected,_,_:=user32.NewProc("TrackPopupMenu").Call(menu,0x0100|0x0002,uintptr(point.X),uintptr(point.Y),0,s.window,0)
	var action func();switch selected{case 1:action=s.actions.Capture;case 5:action=s.actions.CaptureScreen;case 2:action=s.actions.OpenLibrary;case 3:action=s.actions.OpenSettings;case 4:action=s.actions.Quit};if action!=nil{go action()}
	user32.NewProc("PostMessageW").Call(s.window,0,0,0)
}

func screenMetric(index uintptr)int{value,_,_:=user32.NewProc("GetSystemMetrics").Call(index);return int(int32(value))}

func readWindowsFrame()(*windowsFrame,error){
	desktop,err:=readWindowsDesktop();if err!=nil{return nil,err}
	f:=&windowsFrame{x:desktop.x,y:desktop.y,width:desktop.width,height:desktop.height,topology:desktop.topology}
	dc,_,_:=user32.NewProc("GetDC").Call(0);if dc==0{return nil,fmt.Errorf("Нет доступа к экрану Windows")};defer user32.NewProc("ReleaseDC").Call(0,dc)
	f.dc,_,_=gdi32.NewProc("CreateCompatibleDC").Call(dc)
	if f.dc==0{f.close();return nil,fmt.Errorf("Не удалось подготовить контекст захвата")}
	// Capture directly into a known top-down BGRA surface. Display-dependent
	// compatible bitmaps can be copied by BitBlt while GetDIBits still returns
	// zero rows (including on the Windows desktop used for development).
	info:=bitmapInfo{Header:bitmapHeader{Size:uint32(unsafe.Sizeof(bitmapHeader{})),Width:int32(f.width),Height:-int32(f.height),Planes:1,BitCount:32}}
	var bits unsafe.Pointer
	f.bitmap,_,err=gdi32.NewProc("CreateDIBSection").Call(dc,uintptr(unsafe.Pointer(&info)),0,uintptr(unsafe.Pointer(&bits)),0,0)
	if f.bitmap==0||bits==nil{f.close();return nil,fmt.Errorf("Не удалось подготовить bitmap захвата Windows: %v",err)}
	f.previous,_,_=gdi32.NewProc("SelectObject").Call(f.dc,f.bitmap)
	if f.previous==0||f.previous==^uintptr(0){f.previous=0;f.close();return nil,fmt.Errorf("Не удалось выбрать bitmap захвата")}
	if result,_,_:=gdi32.NewProc("BitBlt").Call(f.dc,0,0,uintptr(f.width),uintptr(f.height),dc,uintptr(int64(f.x)),uintptr(int64(f.y)),0x00cc0020|0x40000000);result==0{f.close();return nil,fmt.Errorf("Windows отказал в захвате экрана")}
	// Complete the GDI write before reading unmanaged DIB memory, then retain an
	// owned copy so closing the bitmap never invalidates the captured pixels.
	if flushed,_,_:=gdi32.NewProc("GdiFlush").Call();flushed==0{f.close();return nil,fmt.Errorf("Windows не завершила копирование пикселей экрана")}
	f.pixels=append([]byte(nil),unsafe.Slice((*byte)(bits),f.width*f.height*4)...)
	after,err:=readWindowsDesktop();if err!=nil{f.close();return nil,err}
	if after.x!=f.x||after.y!=f.y||after.width!=f.width||after.height!=f.height||after.topology!=f.topology{f.close();return nil,fmt.Errorf("Дисплеи изменились во время захвата; повтори снимок")}
	return f,nil
}

func(f *windowsFrame)close(){if f.dc!=0&&f.previous!=0{gdi32.NewProc("SelectObject").Call(f.dc,f.previous)};if f.bitmap!=0{gdi32.NewProc("DeleteObject").Call(f.bitmap)};if f.dc!=0{gdi32.NewProc("DeleteDC").Call(f.dc)};f.dc,f.bitmap,f.previous=0,0,0;f.pixels=nil}

func(s *windowsService)beginCapture(ctx context.Context, result chan captureOutcome, fullScreen bool)error{
	if err:=ctx.Err();err!=nil{return err}
	if s.overlay!=0{return fmt.Errorf("Выделение уже открыто")}
	if fullScreen && int64(screenMetric(78))*int64(screenMetric(79))>16_000_000{return fmt.Errorf("Полный снимок превышает 16 млн пикселей; используй захват области")}
	frame,err:=readWindowsFrame();if err!=nil{return err}
	if fullScreen {
		defer frame.close()
		if int64(frame.width)*int64(frame.height)>16_000_000{return fmt.Errorf("Полный снимок превышает 16 млн пикселей; используй захват области")}
		output:=windowsRegion(frame,image.Rect(0,0,frame.width,frame.height))
		go func(){var encoded bytes.Buffer;err:=png.Encode(&encoded,output);result<-captureOutcome{value:Capture{PNG:encoded.Bytes(),MonitorID:"windows-virtual-desktop",Scale:1},err:err}}()
		return nil
	}
	if err:=ctx.Err();err!=nil{frame.close();return err}
	s.frame=frame;s.result=result;s.captureContext=ctx;s.dragging=false
	window,_,err:=user32.NewProc("CreateWindowExW").Call(0x8|0x80,uintptr(unsafe.Pointer(s.className)),0,0x80000000,uintptr(int64(frame.x)),uintptr(int64(frame.y)),uintptr(frame.width),uintptr(frame.height),0,0,s.instance,0)
	if window==0{frame.close();s.frame=nil;s.result=nil;s.captureContext=nil;return fmt.Errorf("Не удалось открыть выделение: %w",err)}
	s.overlay=window;windowsByHandle.Store(window,s)
	user32.NewProc("ShowWindow").Call(window,5);user32.NewProc("SetForegroundWindow").Call(window);user32.NewProc("SetFocus").Call(window);user32.NewProc("UpdateWindow").Call(window)
	if foreground,_,_:=user32.NewProc("GetForegroundWindow").Call();foreground!=window{s.finishCapture(image.Rectangle{},fmt.Errorf("Windows не активировала выделение; повтори захват из окна приложения"));return nil}
	user32.NewProc("SetCursor").Call(s.crosshair)
	return nil
}

func(s *windowsService)overlayProcedure(window uintptr,message uint32,wparam,lparam uintptr)uintptr{
	var point image.Point
	if message==0x0201||message==0x0200||message==0x0202 {
		var err error
		point,err=s.selectionPoint(window);if err!=nil{s.finishCapture(image.Rectangle{},err);return 0}
	}
	switch message{
	case 0x0020:user32.NewProc("SetCursor").Call(s.crosshair);return 1 // WM_SETCURSOR
	case 0x0100:if wparam==27{s.finishCapture(image.Rectangle{},ErrCancelled)};return 0
	case 0x0201:
		s.start=point;s.current=point;s.dragging=true;user32.NewProc("SetCapture").Call(window)
		if captured,_,_:=user32.NewProc("GetCapture").Call();captured!=window{s.finishCapture(image.Rectangle{},fmt.Errorf("Windows не передала мышь окну выделения"))};return 0
	case 0x0200:if s.dragging{s.current=point;user32.NewProc("InvalidateRect").Call(window,0,0)};return 0
	case 0x0202:if s.dragging{s.dragging=false;user32.NewProc("ReleaseCapture").Call();s.finishCapture(image.Rectangle{Min:s.start,Max:point}.Canon(),nil)};return 0
	case 0x0204:s.finishCapture(image.Rectangle{},ErrCancelled);return 0
	case 0x0215:if s.dragging&&lparam!=window{s.finishCapture(image.Rectangle{},ErrCancelled)};return 0 // WM_CAPTURECHANGED
	case 0x001f:s.finishCapture(image.Rectangle{},ErrCancelled);return 0 // WM_CANCELMODE
	case 0x001c:if wparam==0{s.finishCapture(image.Rectangle{},ErrCancelled)};return 0 // WM_ACTIVATEAPP
	case wmDisplayChange,wmDPIChanged:s.finishCapture(image.Rectangle{},fmt.Errorf("Дисплеи или масштаб изменились; повтори выделение области"));return 0
	case wmSettingChange:if err:=s.validateSelectionDesktop();err!=nil{s.finishCapture(image.Rectangle{},err)};return 0
	case wmPowerBroadcast:if wparam==4{s.finishCapture(image.Rectangle{},ErrCancelled)};return 1
	case wmClose:s.finishCapture(image.Rectangle{},ErrCancelled);return 0
	case wmPaint:
		var paint paintStruct;dc,_,_:=user32.NewProc("BeginPaint").Call(window,uintptr(unsafe.Pointer(&paint)))
		if s.frame!=nil{gdi32.NewProc("BitBlt").Call(dc,0,0,uintptr(s.frame.width),uintptr(s.frame.height),s.frame.dc,0,0,0x00cc0020)}
		if s.dragging{rect:=image.Rect(s.start.X,s.start.Y,s.current.X,s.current.Y);pen,_,_:=gdi32.NewProc("CreatePen").Call(0,2,0x50b85a);oldPen,_,_:=gdi32.NewProc("SelectObject").Call(dc,pen);brush,_,_:=gdi32.NewProc("GetStockObject").Call(5);oldBrush,_,_:=gdi32.NewProc("SelectObject").Call(dc,brush);gdi32.NewProc("Rectangle").Call(dc,uintptr(rect.Min.X),uintptr(rect.Min.Y),uintptr(rect.Max.X),uintptr(rect.Max.Y));gdi32.NewProc("SelectObject").Call(dc,oldBrush);gdi32.NewProc("SelectObject").Call(dc,oldPen);gdi32.NewProc("DeleteObject").Call(pen)}
		user32.NewProc("EndPaint").Call(window,uintptr(unsafe.Pointer(&paint)));return 0
	case 0x0014:return 1
	}
	result,_,_:=user32.NewProc("DefWindowProcW").Call(window,uintptr(message),wparam,lparam);return result
}

func(s *windowsService)finishCapture(region image.Rectangle,err error){
	frame,result,window:=s.frame,s.result,s.overlay
	if frame==nil||result==nil{return}
	if err==nil{err=s.validateSelectionDesktop()}
	s.frame=nil;s.result=nil;s.captureContext=nil;s.overlay=0;s.dragging=false
	if captured,_,_:=user32.NewProc("GetCapture").Call();captured==window{user32.NewProc("ReleaseCapture").Call()}
	windowsByHandle.Delete(window);user32.NewProc("DestroyWindow").Call(window);defer frame.close()
	region=region.Intersect(image.Rect(0,0,frame.width,frame.height))
	if err==nil&&region.Empty(){err=ErrCancelled}
	if err==nil&&int64(region.Dx())*int64(region.Dy())>16_000_000{err=fmt.Errorf("Выбери область до 16 млн пикселей")}
	if err!=nil{result<-captureOutcome{err:err};return}
	output:=windowsRegion(frame,region)
	bounds:=core.ScreenRegion{X:frame.x+region.Min.X,Y:frame.y+region.Min.Y,Width:region.Dx(),Height:region.Dy(),DesktopX:frame.x,DesktopY:frame.y,DesktopWidth:frame.width,DesktopHeight:frame.height,DesktopTopology:frame.topology}
	go func(){var encoded bytes.Buffer;err:=png.Encode(&encoded,output);result<-captureOutcome{value:Capture{PNG:encoded.Bytes(),MonitorID:"windows-virtual-desktop",Scale:1,Region:&bounds},err:err}}()
}

func windowsRegion(frame *windowsFrame, region image.Rectangle) *image.NRGBA {
	output:=image.NewNRGBA(image.Rect(0,0,region.Dx(),region.Dy()))
	for y:=0;y<region.Dy();y++{for x:=0;x<region.Dx();x++{source:=((region.Min.Y+y)*frame.width+region.Min.X+x)*4;target:=y*output.Stride+x*4;output.Pix[target]=frame.pixels[source+2];output.Pix[target+1]=frame.pixels[source+1];output.Pix[target+2]=frame.pixels[source];output.Pix[target+3]=255}}
	return output
}
