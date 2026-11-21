package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"skrinpolka/internal/analysis"
	"skrinpolka/internal/core"
)

type OCREvent struct {Token string `json:"token"`;RecordID string `json:"record_id"`;Result core.OCRResult `json:"result"`}

func newSessionID()(string,error){var id [16]byte;if _,err:=rand.Read(id[:]);err!=nil{return "",err};return hex.EncodeToString(id[:]),nil}

func(d *Desktop)GetOCRReadiness()analysis.Readiness{return d.ocrService.Readiness()}

func(d *Desktop)StartDraftOCR(token string)(core.OCRResult,error){
	ctx,err:=d.nativeContext();if err!=nil{return core.OCRResult{},err}
	if ctx.Err()!=nil||d.quitting.Load(){return core.OCRResult{},fmt.Errorf("Приложение завершает работу")}
	d.selection.Lock()
	pending:=d.pending
	if pending==nil||token==""||pending.token!=token{d.selection.Unlock();return core.OCRResult{},fmt.Errorf("Черновик уже закрыт")}
	if pending.renderDirty{d.selection.Unlock();return core.OCRResult{},fmt.Errorf("Картинка редактора ещё обновляется")}
	if pending.photoBusy{d.selection.Unlock();return core.OCRResult{},fmt.Errorf("Сначала дождись удаления фона или останови его")}
	hash:=sha256.Sum256(pending.data);inputHash:=hex.EncodeToString(hash[:])
	return d.startOCRLocked(ctx,pending,inputHash)
}

// Called with selection held; it releases the lock on every path.
func(d *Desktop)startOCRLocked(ctx context.Context,pending *pendingPNG,inputHash string)(core.OCRResult,error){
	if pending.ocr.InputSHA256==inputHash&&(pending.ocr.Status=="queued"||pending.ocr.Status=="running"||pending.ocr.Status=="ready"||pending.ocr.Status=="no_text"){
		result:=pending.ocr;d.selection.Unlock();return result,nil
	}
	previous:=pending.ocr
	readiness:=d.ocrService.Readiness()
	if !readiness.Available{
		pending.ocr=core.OCRResult{Status:"unavailable",Error:readiness.Reason,InputSHA256:inputHash}
		if previous.InputSHA256==inputHash&&previous.HasCorrection{pending.ocr.HasCorrection=true;pending.ocr.CorrectedText=previous.CorrectedText}
		var err error
		if pending.savedID!=""{_,err=d.core.MergeOCRAt(pending.savedLibrary,pending.savedID,inputHash,pending.ocr)}
		result:=pending.ocr;d.selection.Unlock();return result,err
	}
	jobCtx,cancel:=context.WithCancel(ctx)
	if pending.ocrCancel!=nil{pending.ocrCancel()};pending.ocrCancel=cancel
	generation:=pending.generation
	pending.ocr=core.OCRResult{Status:"queued",InputSHA256:inputHash,Engine:"tesseract",ModelRevision:d.ocrService.ModelRevision(),SessionID:d.sessionID}
	if previous.InputSHA256==inputHash&&previous.HasCorrection{pending.ocr.HasCorrection=true;pending.ocr.CorrectedText=previous.CorrectedText}
	result:=pending.ocr
	// Persist the queue state before the worker can finish. Writing it after
	// launching the goroutine could replace a fresh result with stale "queued".
	if pending.savedID!=""{
		if _,err:=d.core.MergeOCRAt(pending.savedLibrary,pending.savedID,inputHash,pending.ocr);err!=nil{
			pending.ocr.Status="error";pending.ocr.Error=err.Error();result=pending.ocr
			d.selection.Unlock();cancel();return result,err
		}
	}
	d.jobsGate.Lock()
	if d.quitting.Load(){d.jobsGate.Unlock();d.selection.Unlock();cancel();return core.OCRResult{},fmt.Errorf("Приложение завершает работу")}
	d.jobs.Add(1);d.jobsGate.Unlock()
	d.selection.Unlock()
	go func(){
		defer d.jobs.Done();defer cancel()
		defer func(){d.selection.Lock();if pending.generation==generation&&pending.savedID!=""{key:=pending.savedLibrary+"|"+pending.savedID;if d.recordJobs[key]==pending{delete(d.recordJobs,key)}};d.selection.Unlock()}()
		text,err:=d.ocrService.Recognize(jobCtx,func()([]byte,error){
			d.selection.Lock()
			if pending.generation!=generation{d.selection.Unlock();return nil,context.Canceled}
			id,library,data:=pending.savedID,pending.savedLibrary,pending.data
			d.selection.Unlock()
			if id!=""{return d.core.ReadPNGAt(library,id)}
			if data==nil{return nil,context.Canceled};return data,nil
		},func(){
			d.selection.Lock();valid:=pending.generation==generation&&(d.pending==pending||pending.savedID!="");if valid{pending.ocr.Status="running"};event:=OCREvent{Token:pending.token,RecordID:pending.savedID,Result:pending.ocr};d.selection.Unlock()
			if valid&&ctx.Err()==nil{wruntime.EventsEmit(ctx,"ocr:update",event)}
		})
		d.selection.Lock()
		if pending.generation!=generation||(d.pending!=pending&&pending.savedID==""){d.selection.Unlock();return}
		pending.ocr.Status="ready";pending.ocr.RawText=text;pending.ocr.CompletedAt=time.Now().UTC().Format(time.RFC3339Nano)
		if strings.TrimSpace(text)==""{pending.ocr.Status="no_text"}
		if err!=nil{pending.ocr.Status="error";pending.ocr.Error=err.Error();if errors.Is(err,context.Canceled){pending.ocr.Status="cancelled"}}
		event:=OCREvent{Token:pending.token,RecordID:pending.savedID,Result:pending.ocr};library:=pending.savedLibrary
		if event.RecordID!=""{
			if _,err:=d.core.MergeOCRAt(library,event.RecordID,inputHash,event.Result);err!=nil{
				event.Result.Error="Снимок сохранён, но OCR не записан: "+err.Error();event.Result.Status="error"
				pending.ocr=event.Result
			}
		}
		d.selection.Unlock()
		if event.Result.Status=="error"&&strings.HasPrefix(event.Result.Error,"Снимок сохранён, но OCR не записан:")&&ctx.Err()==nil{wruntime.EventsEmit(ctx,"ocr:warning",event.Result.Error)}
		if ctx.Err()==nil{wruntime.EventsEmit(ctx,"ocr:update",event)}
	}()
	return result,nil
}

func(d *Desktop)RunRecordOCR(id string)(core.OCRResult,error){
	d.operation.Lock();defer d.operation.Unlock()
	return d.runRecordOCRLocked(id)
}

func(d *Desktop)runRecordOCRLocked(id string)(core.OCRResult,error){
	return d.startRecordOCRLocked(id,true)
}

func(d *Desktop)startRecordOCRLocked(id string,force bool)(core.OCRResult,error){
	ctx,err:=d.nativeContext();if err!=nil{return core.OCRResult{},err}
	if ctx.Err()!=nil||d.quitting.Load(){return core.OCRResult{},fmt.Errorf("Приложение завершает работу")}
	record,err:=d.core.GetRecord(id);if err!=nil{return core.OCRResult{},err}
	if !force&&record.OCR.InputSHA256==record.ImageSHA256&&(record.OCR.Status=="ready"||record.OCR.Status=="no_text"||record.OCR.Status=="cancelled"){return record.OCR,nil}
	library:=d.core.CurrentSettings().LibraryDirectory
	d.selection.Lock()
	key:=library+"|"+id
	if existing:=d.recordJobs[key];existing!=nil{result:=existing.ocr;d.selection.Unlock();return result,nil}
	pending:=&pendingPNG{token:"record:"+id,savedID:id,savedLibrary:library,generation:1,ocr:record.OCR}
	pending.ocr.Status="idle" // New job; manual correction remains separate.
	d.recordJobs[key]=pending
	result,err:=d.startOCRLocked(ctx,pending,record.ImageSHA256)
	if err!=nil||result.Status=="unavailable"{d.selection.Lock();delete(d.recordJobs,key);d.selection.Unlock()}
	return result,err
}

// Every desktop save enters this path, independently of frontend preferences.
// Caller holds operation, but not selection. Only queueing is synchronous;
// OCR continues against the saved PNG and never delays saving for its result.
func(d *Desktop)ensureSavedOCRLocked(record core.Screenshot)core.Screenshot{
	result,err:=d.startRecordOCRLocked(record.ID,false)
	if err!=nil{
		if ctx,e:=d.nativeContext();e==nil&&ctx.Err()==nil{wruntime.EventsEmit(ctx,"ocr:warning","Снимок сохранён; OCR не запущен: "+err.Error())}
		return record
	}
	if updated,e:=d.core.GetRecord(record.ID);e==nil{return updated}
	record.OCR=result
	return record
}

func(d *Desktop)InvalidateDraftOCR(token string,clientRevision uint64){
	d.selection.Lock();defer d.selection.Unlock()
	if d.pending==nil||d.pending.token!=token{return}
	if clientRevision<d.pending.clientRevision{return}
	if d.pending.ocrCancel!=nil{d.pending.ocrCancel()}
	d.pending.generation++;d.pending.ocr=core.OCRResult{Status:"idle"};d.pending.clientRevision=clientRevision;d.pending.renderDirty=true
}

func(d *Desktop)CancelDraftOCR(token string){
	d.selection.Lock();defer d.selection.Unlock()
	if d.pending!=nil&&d.pending.token==token{if d.pending.ocrCancel!=nil{d.pending.ocrCancel()};d.pending.generation++;d.pending.ocr.Status="cancelled"}
}

func(d *Desktop)SaveDraftOCRText(token,text string)error{
	if !utf8.ValidString(text)||len(text)>512<<10{return fmt.Errorf("Текст превышает 512 KiB")}
	d.selection.Lock();defer d.selection.Unlock()
	if d.pending==nil||d.pending.token!=token{return fmt.Errorf("Черновик уже закрыт")}
	d.pending.ocr.HasCorrection=true;d.pending.ocr.CorrectedText=text
	if d.pending.ocr.InputSHA256==""{sum:=sha256.Sum256(d.pending.data);d.pending.ocr.InputSHA256=hex.EncodeToString(sum[:]);d.pending.ocr.Status="manual"}
	return nil
}

func(d *Desktop)CopyOCRText(text string)error{
	if !utf8.ValidString(text)||len(text)>512<<10{return fmt.Errorf("Текст превышает 512 KiB")}
	ctx,err:=d.nativeContext();if err!=nil{return err}
	return wruntime.ClipboardSetText(ctx,text)
}

func(d *Desktop)ExportOCRText(text string)(bool,error){
	if !utf8.ValidString(text)||len(text)>512<<10{return false,fmt.Errorf("Текст превышает 512 KiB")}
	d.dialog.Lock();defer d.dialog.Unlock()
	ctx,err:=d.nativeContext();if err!=nil{return false,err}
	path,err:=wruntime.SaveFileDialog(ctx,wruntime.SaveDialogOptions{Title:"Сохранить распознанный текст",DefaultFilename:"ScreenShooter Pro text.txt",Filters:[]wruntime.FileFilter{{DisplayName:"Текст UTF-8",Pattern:"*.txt"}}})
	if err!=nil||path==""{return false,err}
	file,err:=os.CreateTemp(filepath.Dir(path),".skrinpolka-text-*.tmp");if err!=nil{return false,err};defer os.Remove(file.Name())
	if _,err:=file.WriteString(text);err!=nil{file.Close();return false,err};if err:=file.Sync();err!=nil{file.Close();return false,err};if err:=file.Close();err!=nil{return false,err}
	if err:=os.Rename(file.Name(),path);err!=nil{return false,err};return true,nil
}
