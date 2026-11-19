package main

import (
	"bytes"
	"crypto/rand"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"path/filepath"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"skrinpolka/internal/core"
	"skrinpolka/internal/platform"
)

type pendingPNG struct {
	token string
	data  []byte
	ocr core.OCRResult
	ocrCancel context.CancelFunc
	generation uint64
	savedID string
	savedLibrary string
	clientRevision uint64
	renderDirty bool
	fromCapture bool
	focusTarget platform.FocusTarget
	photoCancel context.CancelFunc
	photoBusy bool
	photoRequest uint64
	photoStopped uint64
}

type SelectedPNG struct {
	Name           string `json:"name"`
	PreviewDataURL string `json:"preview_data_url"`
	Token          string `json:"token"`
	FromCapture    bool `json:"from_capture"`
}

func (d *Desktop) SelectPNG() (SelectedPNG, error) {
	d.dialog.Lock()
	defer d.dialog.Unlock()
	ctx, err := d.nativeContext()
	if err != nil {
		return SelectedPNG{}, err
	}
	source, err := wruntime.OpenFileDialog(ctx, wruntime.OpenDialogOptions{
		Title: "Выбери PNG для предсохранения",
		Filters: []wruntime.FileFilter{{DisplayName: "Изображение PNG", Pattern: "*.png"}},
	})
	if err != nil || source == "" {
		return SelectedPNG{}, err
	}
	d.operation.Lock()
	defer d.operation.Unlock()
	data, err := core.PreparePNG(source)
	if err != nil {
		return SelectedPNG{}, err
	}
	return d.prepareSelection(data, filepath.Base(source))
}

func (d *Desktop) prepareSelection(data []byte, name string) (SelectedPNG,error) {
	preview, err := previewDataURL(data)
	if err != nil {
		return SelectedPNG{}, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return SelectedPNG{}, fmt.Errorf("Не удалось подготовить черновик: %w", err)
	}
	token := hex.EncodeToString(random[:])
	d.selection.Lock()
	if d.pending != nil { d.selection.Unlock(); return SelectedPNG{}, fmt.Errorf("Сначала сохрани или отмени текущий снимок") }
	d.pending = &pendingPNG{token: token, data: data, ocr:core.OCRResult{Status:"idle"},generation:1}
	d.selection.Unlock()
	return SelectedPNG{Name: name, PreviewDataURL: preview, Token: token}, nil
}

func (d *Desktop) DiscardSelectedPNG(token string) {
	d.selection.Lock()
	defer d.selection.Unlock()
	if d.pending != nil && d.pending.token == token {
		if d.pending.ocrCancel!=nil{d.pending.ocrCancel()}
		if d.pending.photoCancel!=nil{d.pending.photoCancel()}
		d.pending = nil
	}
}

func (d *Desktop) GetDraftImageData(token string)(string,error){
	d.selection.Lock();defer d.selection.Unlock()
	if d.pending==nil||token==""||d.pending.token!=token{return "",fmt.Errorf("Черновик уже закрыт")}
	return "data:image/png;base64,"+base64.StdEncoding.EncodeToString(d.pending.data),nil
}

func (d *Desktop) UpdateDraftPNG(token,dataURL string,clientRevision uint64)error{
	d.operation.Lock();defer d.operation.Unlock()
	d.selection.Lock();defer d.selection.Unlock()
	if d.pending==nil||token==""||d.pending.token!=token{return fmt.Errorf("Черновик уже закрыт")}
	if d.pending.photoBusy{return fmt.Errorf("Дождись удаления фона или останови его")}
	if clientRevision<d.pending.clientRevision{return fmt.Errorf("Картинка редактора уже обновлена")}
	const prefix="data:image/png;base64,"
	if !strings.HasPrefix(dataURL,prefix)||len(dataURL)>int(core.MaxPNGBytes)*4/3+1024{return fmt.Errorf("PNG редактора превышает лимит 20 MiB")}
	raw,err:=base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL,prefix));if err!=nil{return fmt.Errorf("Не удалось прочитать результат редактора")}
	data,err:=core.PreparePNGData(raw);if err!=nil{return err}
	if !bytes.Equal(data,d.pending.data){
		if d.pending.ocrCancel!=nil{d.pending.ocrCancel()}
		d.pending.generation++
		d.pending.ocr=core.OCRResult{Status:"idle"}
	}
	d.pending.data=data
	d.pending.clientRevision=clientRevision;d.pending.renderDirty=false
	return nil
}

func (d *Desktop) ImportSelectedPNG(token, note string) (core.Screenshot, error) {
	d.operation.Lock()
	defer d.operation.Unlock()
	d.selection.Lock()
	if d.pending == nil || d.pending.token != token || token == "" {
		d.selection.Unlock()
		return core.Screenshot{}, fmt.Errorf("Черновик уже закрыт. Выбери PNG заново")
	}
	if d.pending.renderDirty{d.selection.Unlock();return core.Screenshot{},fmt.Errorf("Перед сохранением нужна актуальная картинка редактора")}
	if d.pending.photoBusy{d.selection.Unlock();return core.Screenshot{},fmt.Errorf("Дождись удаления фона или останови его")}
	pending:=d.pending
	library:=d.core.CurrentSettings().LibraryDirectory
	record, err := d.core.ImportPNGData(pending.data, note)
	if err != nil {
		d.selection.Unlock()
		return core.Screenshot{}, err
	}
	pending.savedID=record.ID;pending.savedLibrary=library
	if pending.ocr.InputSHA256!=record.ImageSHA256{
		if pending.ocrCancel!=nil{pending.ocrCancel()}
		pending.generation++;pending.ocr=core.OCRResult{Status:"idle"}
	}
	if pending.ocr.Status=="queued"||pending.ocr.Status=="running"{d.recordJobs[library+"|"+record.ID]=pending}
	if pending.ocr.InputSHA256==record.ImageSHA256{
		if updated,err:=d.core.MergeOCRAt(library,record.ID,record.ImageSHA256,pending.ocr);err==nil{record=updated}else{if ctx,e:=d.nativeContext();e==nil{wruntime.EventsEmit(ctx,"ocr:warning","Снимок сохранён; запись OCR отложена: "+err.Error())}}
	}
	d.pending = nil
	if pending.fromCapture { d.completedCapture = &savedCapture{token:pending.token, target:pending.focusTarget} }
	pending.data=nil
	d.selection.Unlock()
	return d.ensureSavedOCRLocked(record), nil
}

// GetImageData returns a bounded preview, not a file:// URL or an arbitrary path.
func (d *Desktop) GetImageData(id string) (string, error) {
	d.operation.Lock()
	defer d.operation.Unlock()
	data, err := d.core.ReadPNG(id)
	if err != nil {
		return "", err
	}
	return previewDataURL(data)
}

func previewDataURL(data []byte) (string, error) {
	return previewDataURLMax(data,1280)
}

func previewDataURLMax(data []byte,maxSide int) (string, error) {
	source, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("Не удалось подготовить предпросмотр PNG: %w", err)
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width > maxSide || height > maxSide {
		previewWidth, previewHeight := width, height
		if width >= height {
			previewWidth = maxSide
			previewHeight = max(1, height*maxSide/width)
		} else {
			previewHeight = maxSide
			previewWidth = max(1, width*maxSide/height)
		}
		preview := image.NewNRGBA(image.Rect(0, 0, previewWidth, previewHeight))
		for y := 0; y < previewHeight; y++ {
			for x := 0; x < previewWidth; x++ {
				preview.Set(x, y, source.At(bounds.Min.X+x*width/previewWidth, bounds.Min.Y+y*height/previewHeight))
			}
		}
		source = preview
	}
	var output bytes.Buffer
	if err := png.Encode(&output, source); err != nil {
		return "", fmt.Errorf("Не удалось закодировать предпросмотр: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(output.Bytes()), nil
}
