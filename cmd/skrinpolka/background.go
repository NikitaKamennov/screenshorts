package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image/png"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"skrinpolka/internal/analysis"
	"skrinpolka/internal/core"
)

type BackgroundResult struct {PNG string `json:"png"`;InputSHA256 string `json:"input_sha256"`}
type BackgroundEvent struct {Token string `json:"token"`;RequestID uint64 `json:"request_id"`;Status string `json:"status"`}

func(d *Desktop)GetBackgroundReadiness()analysis.Readiness{return d.backgroundService.Readiness()}

func(d *Desktop)CancelBackgroundRemoval(token string,requestID uint64){
	d.selection.Lock();defer d.selection.Unlock()
	if d.pending==nil||d.pending.token!=token||requestID==0{return}
	// Remember a stop even when it arrives before the start goroutine. A late
	// cancellation for an older request must not stop the next request.
	d.pending.photoStopped=max(d.pending.photoStopped,requestID)
	if d.pending.photoRequest==requestID&&d.pending.photoCancel!=nil{d.pending.photoCancel()}
}

func(d *Desktop)RemoveDraftBackground(token string,revision,requestID uint64)(BackgroundResult,error){
	ctx,err:=d.nativeContext();if err!=nil{return BackgroundResult{},err}
	if ready:=d.backgroundService.Readiness();!ready.Available{return BackgroundResult{},fmt.Errorf("%s",ready.Reason)}
	d.selection.Lock()
	pending:=d.pending
	if pending==nil||token==""||pending.token!=token{d.selection.Unlock();return BackgroundResult{},fmt.Errorf("Черновик уже закрыт")}
	if requestID==0||requestID<=pending.photoStopped||requestID<=pending.photoRequest{d.selection.Unlock();return BackgroundResult{},context.Canceled}
	if pending.photoBusy||pending.renderDirty||pending.clientRevision!=revision{d.selection.Unlock();return BackgroundResult{},fmt.Errorf("Сначала дождись обновления картинки редактора")}
	if pending.ocr.Status=="queued"||pending.ocr.Status=="running"{
		if pending.ocrCancel!=nil{pending.ocrCancel()};pending.generation++
		pending.ocr=core.OCRResult{Status:"idle",InputSHA256:pending.ocr.InputSHA256,HasCorrection:pending.ocr.HasCorrection,CorrectedText:pending.ocr.CorrectedText}
	}
	input,generation:=pending.data,pending.generation
	hash:=sha256.Sum256(input);inputHash:=hex.EncodeToString(hash[:])
	jobCtx,cancel:=context.WithCancel(ctx);pending.photoCancel=cancel;pending.photoBusy=true;pending.photoRequest=requestID
	d.jobsGate.Lock()
	if d.quitting.Load(){d.jobsGate.Unlock();pending.photoBusy=false;pending.photoCancel=nil;d.selection.Unlock();cancel();return BackgroundResult{},fmt.Errorf("Приложение завершает работу")}
	d.jobs.Add(1);d.jobsGate.Unlock();d.selection.Unlock()
	defer d.jobs.Done();defer cancel()
	defer func(){d.selection.Lock();if pending.photoRequest==requestID{pending.photoBusy=false;pending.photoCancel=nil};d.selection.Unlock()}()
	wruntime.EventsEmit(ctx,"background:update",BackgroundEvent{Token:token,RequestID:requestID,Status:"queued"})
	output,err:=d.backgroundService.Remove(jobCtx,input,func(){if jobCtx.Err()==nil{wruntime.EventsEmit(ctx,"background:update",BackgroundEvent{Token:token,RequestID:requestID,Status:"running"})}})
	if err!=nil{return BackgroundResult{},err}
	if err:=jobCtx.Err();err!=nil{return BackgroundResult{},err}
	before,err:=png.DecodeConfig(bytes.NewReader(input));if err!=nil{return BackgroundResult{},err}
	after,err:=png.DecodeConfig(bytes.NewReader(output));if err!=nil||before.Width!=after.Width||before.Height!=after.Height{return BackgroundResult{},fmt.Errorf("Размер результата удаления фона не совпадает со снимком")}
	d.selection.Lock();valid:=d.pending==pending&&pending.generation==generation&&pending.clientRevision==revision&&!pending.renderDirty&&pending.photoStopped<requestID&&jobCtx.Err()==nil;d.selection.Unlock()
	if !valid{return BackgroundResult{},fmt.Errorf("Снимок изменился во время удаления фона; результат не применён")}
	return BackgroundResult{PNG:"data:image/png;base64,"+base64.StdEncoding.EncodeToString(output),InputSHA256:inputHash},nil
}
