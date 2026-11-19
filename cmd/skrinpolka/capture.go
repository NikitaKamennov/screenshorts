package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"skrinpolka/internal/core"
	"skrinpolka/internal/platform"
)

func (d *Desktop) nativeService() platform.Service {d.nativeMu.RLock();defer d.nativeMu.RUnlock();return d.native}

func (d *Desktop) domReady(_ context.Context) {
	ctx,err:=d.nativeContext();if err!=nil{return}
	if !d.nativeStarted.CompareAndSwap(false,true){return}
	d.showCentered()
	d.applyNativeTheme(d.core.CurrentSettings().Theme)
	d.jobsGate.Lock()
	if d.quitting.Load() { d.jobsGate.Unlock(); return }
	d.jobs.Add(1)
	d.jobsGate.Unlock()
	go func(){
		defer d.jobs.Done()
		startupOK := true
		if err:=d.core.InterruptOldOCR(d.sessionID);err!=nil{startupOK=false;d.notifyError(err)}
		service,err:=platform.New(platform.Actions{
			Capture:func(){if err:=d.CaptureArea();err!=nil&&!errors.Is(err,platform.ErrCancelled){d.notifyError(err)}},
			CaptureScreen:func(){if err:=d.CaptureScreen();err!=nil&&!errors.Is(err,platform.ErrCancelled){d.notifyError(err)}},
			CaptureSilent:func(){if err:=d.CaptureSilent();err!=nil{d.notifyQuietError(err)}},
			CaptureSilentArea:func(){if err:=d.CaptureSilentArea();err!=nil{d.notifyQuietError(err)}},
			OpenLibrary:d.show,
			OpenSettings:func(){d.show();wruntime.EventsEmit(ctx,"native:settings")},
			Quit:d.Quit,
			Changed:func(caps platform.Capabilities){if !caps.Tray.Available{d.show()};wruntime.EventsEmit(ctx,"native:capabilities",caps)},
		})
		if err!=nil {
			d.nativeMu.Lock();d.nativeProblem=err.Error();d.nativeMu.Unlock()
			if ctx.Err()==nil&&!d.quitting.Load(){wruntime.EventsEmit(ctx,"native:capabilities",platform.Unavailable(err.Error()))}
			d.notifyError(err);return
		}
		d.nativeMu.Lock()
		if ctx.Err()!=nil||d.quitting.Load(){d.nativeMu.Unlock();service.Close();return}
		d.native=service
		d.nativeMu.Unlock()
		settings:=d.core.CurrentSettings()
		shortcut:=settings.CaptureShortcut
		if shortcut=="" && !settings.SetupComplete {shortcut=platform.DefaultShortcut}
		silent:=settings.SilentShortcut
		silentArea:=settings.SilentAreaShortcut
		if !service.Capabilities().SilentCapture.Available { silent="";silentArea="" }
		if _,err:=service.SetShortcuts(ctx,platform.Shortcuts{Area:shortcut,Screen:settings.ScreenShortcut,Silent:silent,SilentArea:silentArea});err!=nil{startupOK=false;d.notifyError(err)}
		caps:=service.Capabilities()
		wruntime.EventsEmit(ctx,"native:capabilities",caps)
		if startupOK&&settings.StartInTray&&settings.SetupComplete { d.hideToTray() }
	}()
}

func (d *Desktop) notifyError(err error){if ctx,e:=d.nativeContext();e==nil&&ctx.Err()==nil&&!d.quitting.Load(){d.show();wruntime.EventsEmit(ctx,"native:error",err.Error())}}

func (d *Desktop) beforeClose(ctx context.Context)bool{
	if d.quitting.Load(){return false}
	return d.hideToTray()
}

func (d *Desktop) Quit(){d.quitting.Store(true);if ctx,err:=d.nativeContext();err==nil{wruntime.Quit(ctx)}}

func (d *Desktop) CaptureArea()error{return d.capture(false)}
func (d *Desktop) CaptureScreen()error{return d.capture(true)}

func (d *Desktop) capture(fullScreen bool)error{
	if d.quitting.Load(){return fmt.Errorf("Приложение завершает работу")}
	if !d.capturing.CompareAndSwap(false,true){return nil}
	defer d.capturing.Store(false)
	d.selection.Lock();hasDraft:=d.pending!=nil;d.selection.Unlock()
	if hasDraft{d.show();if ctx,err:=d.nativeContext();err==nil{wruntime.EventsEmit(ctx,"native:resume-draft")};return nil}
	service:=d.nativeService();if service==nil{return fmt.Errorf("Системные адаптеры ещё запускаются")}
	ctx,err:=d.nativeContext();if err!=nil{return err}
	target:=platform.RememberFocus(ctx)
	d.selection.Lock();d.completedCapture=nil;d.selection.Unlock()
	wasVisible:=d.visible.Load()
	wruntime.WindowHide(ctx);d.visible.Store(false)
	select{case <-ctx.Done():return ctx.Err();case <-time.After(150*time.Millisecond):}
	captureCtx,cancel:=context.WithTimeout(ctx,3*time.Minute);defer cancel()
	var capture platform.Capture
	if fullScreen { capture,err=service.CaptureScreen(captureCtx) } else { capture,err=service.CaptureArea(captureCtx) }
	if err!=nil{if wasVisible||!service.Capabilities().Tray.Available{d.show()};return err}
	d.operation.Lock()
	data,err:=core.PreparePNGData(capture.PNG)
	var selected SelectedPNG
	if err==nil{selected,err=d.prepareSelection(data,core.ScreenshotName(time.Now())+".png")}
	if err==nil {
		d.selection.Lock();d.pending.fromCapture=true;d.pending.focusTarget=target;d.selection.Unlock()
		selected.FromCapture=true
	}
	d.operation.Unlock()
	d.showCentered()
	if err!=nil{return err}
	wruntime.EventsEmit(ctx,"capture:ready",selected)
	return nil
}
