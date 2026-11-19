package main

import (
	"context"
	"fmt"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"skrinpolka/internal/core"
	"skrinpolka/internal/platform"
)

// Quiet failures are retained by the frontend for the next explicit visit to
// the library. Never call notifyError here: that path activates the app window.
func (d *Desktop) notifyQuietError(err error) {
	if ctx, e := d.nativeContext(); e == nil && ctx.Err() == nil && !d.quitting.Load() {
		wruntime.EventsEmit(ctx, "capture:silent-error", "Тихий снимок: " + err.Error())
	}
}

// CaptureSilent bypasses the editor and never shows, hides, centres or focuses
// any window. An existing draft remains intact. Unsupported portals refuse
// this operation rather than falling back to an interactive screenshot.
func (d *Desktop) CaptureSilent() error {
	return d.captureQuiet(false)
}

func (d *Desktop) CaptureSilentArea() error {
	return d.captureQuiet(true)
}

func (d *Desktop) captureQuiet(area bool) error {
	if d.quitting.Load() { return fmt.Errorf("Приложение завершает работу") }
	if !d.capturing.CompareAndSwap(false, true) { return fmt.Errorf("Уже выполняется другой захват; тихий снимок не сделан") }
	defer d.capturing.Store(false)
	service := d.nativeService()
	if service == nil { return fmt.Errorf("Системные адаптеры ещё запускаются") }
	if feature := service.Capabilities().SilentCapture; !feature.Available { return fmt.Errorf("%s", feature.Reason) }
	ctx, err := d.nativeContext(); if err != nil { return err }
	captureCtx, cancel := context.WithTimeout(ctx, 30*time.Second); defer cancel()
	var capture platform.Capture
	if area {
		region:=d.core.CurrentSettings().SilentRegion
		if region==nil{return fmt.Errorf("Сначала задай область тихого скриншота в настройках")}
		capture,err=service.CaptureRegion(captureCtx,*region)
	} else { capture,err=service.CaptureSilent(captureCtx) }
	if err != nil { return err }
	if err := captureCtx.Err(); err != nil { return err }

	d.operation.Lock()
	data, err := core.PreparePNGData(capture.PNG)
	var record core.Screenshot
	if err == nil { record, err = d.core.ImportPNGData(data, "") }
	if err == nil { record = d.ensureSavedOCRLocked(record) }
	d.operation.Unlock()
	if err != nil { return err }
	if ctx.Err() == nil { wruntime.EventsEmit(ctx, "capture:silent-saved") }
	return nil
}

// Only setup is interactive. Subsequent region hotkeys never enter this path.
// The selected pixels are discarded; settings retain only desktop coordinates.
func (d *Desktop) SelectSilentRegion() (core.ScreenRegion,error) {
	if d.quitting.Load(){return core.ScreenRegion{},fmt.Errorf("Приложение завершает работу")}
	if !d.capturing.CompareAndSwap(false,true){return core.ScreenRegion{},fmt.Errorf("Уже выполняется другой захват")};defer d.capturing.Store(false)
	service:=d.nativeService();if service==nil{return core.ScreenRegion{},fmt.Errorf("Системные адаптеры ещё запускаются")}
	if feature:=service.Capabilities().SilentCapture;!feature.Available{return core.ScreenRegion{},fmt.Errorf("%s",feature.Reason)}
	ctx,err:=d.nativeContext();if err!=nil{return core.ScreenRegion{},err}
	defer func(){if ctx.Err()==nil&&!d.quitting.Load(){d.showCentered()}}()
	wruntime.WindowHide(ctx);d.visible.Store(false)
	select{case <-ctx.Done():return core.ScreenRegion{},ctx.Err();case <-time.After(150*time.Millisecond):}
	selectCtx,cancel:=context.WithTimeout(ctx,3*time.Minute);defer cancel()
	capture,err:=service.CaptureArea(selectCtx);if err!=nil{return core.ScreenRegion{},err}
	if capture.Region==nil{return core.ScreenRegion{},fmt.Errorf("Адаптер не вернул координаты выделения")}
	d.operation.Lock();defer d.operation.Unlock()
	return d.core.SaveSilentRegion(*capture.Region)
}
