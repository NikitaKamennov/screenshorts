package main

import (
	"context"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"skrinpolka/internal/platform"
)

type savedCapture struct { token string; target platform.FocusTarget }
type ReturnResult struct { Hidden bool `json:"hidden"`; Message string `json:"message"` }

// This is called only after the PNG/JSON pair has been committed. A late save
// response must never hide a newer capture, another draft, or an imported PNG.
func (d *Desktop) FinishSavedCapture(token string) ReturnResult {
	d.operation.Lock(); defer d.operation.Unlock()
	if !d.capturing.CompareAndSwap(false, true) { return ReturnResult{} }; defer d.capturing.Store(false)
	d.selection.Lock()
	saved := d.completedCapture
	if saved == nil || token == "" || saved.token != token || d.pending != nil { d.selection.Unlock(); return ReturnResult{} }
	d.completedCapture = nil
	d.selection.Unlock()
	ctx, err := d.nativeContext(); if err != nil || ctx.Err() != nil || d.quitting.Load() { return ReturnResult{} }
	if !d.hideToTray() {
		d.windowMu.Lock()
		wruntime.WindowMinimise(ctx)
		d.visible.Store(false)
		d.windowMu.Unlock()
	}
	// Native window operations are queued on the UI thread. Let hide/minimise
	// take effect before asking the OS to activate the remembered window.
	select { case <-ctx.Done(): return ReturnResult{Hidden:true}; case <-time.After(120*time.Millisecond): }
	if saved.target != nil {
		restoreCtx, cancel := context.WithTimeout(ctx, time.Second); defer cancel()
		if err := saved.target.Restore(restoreCtx); err != nil { return ReturnResult{Hidden:true, Message:err.Error()} }
	}
	return ReturnResult{Hidden:true}
}
