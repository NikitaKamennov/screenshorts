package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
	"unicode/utf8"
)

// MergeOCR owns only the OCR field and metadata revision. User comments and
// unfamiliar extensions survive; results for other pixels are rejected.
func(a *App)MergeOCR(id,imageHash string,result OCRResult)(Screenshot,error){
	a.mu.Lock();defer a.mu.Unlock()
	return a.mergeOCR(a.settings.LibraryDirectory,id,imageHash,result)
}

func(a *App)MergeOCRAt(library,id,imageHash string,result OCRResult)(Screenshot,error){
	a.mu.Lock();defer a.mu.Unlock()
	return a.mergeOCR(library,id,imageHash,result)
}

func(a *App)mergeOCR(library,id,imageHash string,result OCRResult)(Screenshot,error){
	if !validID(id)||imageHash==""{return Screenshot{},fmt.Errorf("Не указан снимок OCR")}
	root,err:=os.OpenRoot(library);if err!=nil{return Screenshot{},err};defer root.Close()
	record,err:=loadScreenshot(root,id);if err!=nil{return Screenshot{},err}
	if record.ImageSHA256!=imageHash||result.InputSHA256!=imageHash{return Screenshot{},fmt.Errorf("Результат OCR относится к другой ревизии картинки")}
	imageBytes,err:=readRegular(root,record.ImageFile,MaxPNGBytes);if err!=nil{return Screenshot{},err}
	actual:=sha256.Sum256(imageBytes);if hex.EncodeToString(actual[:])!=imageHash{return Screenshot{},fmt.Errorf("PNG изменён: результат OCR отклонён")}
	if !utf8.ValidString(result.RawText)||!utf8.ValidString(result.CorrectedText)||len(result.RawText)>512<<10||len(result.CorrectedText)>512<<10{return Screenshot{},fmt.Errorf("Текст OCR слишком большой или повреждён")}
	if record.OCR.InputSHA256==imageHash&&record.OCR.HasCorrection&&!result.HasCorrection{result.HasCorrection=true;result.CorrectedText=record.OCR.CorrectedText}
	content,err:=readRegular(root,id+".png.json",maxMetadataBytes);if err!=nil{return Screenshot{},err}
	var fields map[string]json.RawMessage;if err:=decodeJSON(content,&fields);err!=nil{return Screenshot{},err}
	fields["ocr"],err=json.Marshal(result);if err!=nil{return Screenshot{},err}
	fields["metadata_revision"],_=json.Marshal(record.MetadataRevision+1)
	fields["image_revision"],_=json.Marshal(record.ImageRevision)
	fields["updated_at"],_=json.Marshal(time.Now().UTC().Format(time.RFC3339Nano))
	delete(fields,"metadata_digest")
	updated,err:=encodeJSON(fields);if err!=nil{return Screenshot{},err};if int64(len(updated))>maxMetadataBytes{return Screenshot{},fmt.Errorf("Метаданные OCR превышают 1 MiB")}
	name,err:=writeTemporary(root,updated);if err!=nil{return Screenshot{},err};defer root.Remove(name)
	// Own writes are serialized by App.mu; external edits are checked before commit.
	current,err:=loadScreenshot(root,id);if err!=nil{return Screenshot{},err};if current.MetadataDigest!=record.MetadataDigest{return Screenshot{},ErrRecordChanged}
	if err:=root.Rename(name,id+".png.json");err!=nil{return Screenshot{},err}
	return loadScreenshot(root,id)
}

func EffectiveOCR(result OCRResult)string{if result.HasCorrection{return result.CorrectedText};return result.RawText}

func(a *App)GetRecord(id string)(Screenshot,error){
	a.mu.Lock();defer a.mu.Unlock()
	root,err:=os.OpenRoot(a.settings.LibraryDirectory);if err!=nil{return Screenshot{},err};defer root.Close();return loadScreenshot(root,id)
}

func(a *App)InterruptOldOCR(session string)error{
	a.mu.Lock();defer a.mu.Unlock()
	records,_,err:=a.listScreenshots();if err!=nil{return err}
	for _,record:=range records{
		if (record.OCR.Status=="queued"||record.OCR.Status=="running")&&record.OCR.SessionID!=session{
			result:=record.OCR;result.Status="interrupted";result.Error="Предыдущая сессия завершилась до конца OCR. Повтори обработку явно."
			if _,err:=a.mergeOCR(a.settings.LibraryDirectory,record.ID,record.ImageSHA256,result);err!=nil{return err}
		}
	}
	return nil
}
