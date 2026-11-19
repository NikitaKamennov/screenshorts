package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrRecordChanged = errors.New("Запись изменилась. Открой её заново и повтори правку")

// UpdateNote modifies only owned fields, retaining unfamiliar extension fields.
// The expected digest prevents a stale editor from overwriting a newer note.
func (a *App) UpdateNote(id, expectedDigest, note string)(Screenshot,error){
	a.mu.Lock();defer a.mu.Unlock()
	if !validID(id)||expectedDigest==""{return Screenshot{},fmt.Errorf("Не указан актуальный снимок")}
	if !utf8.ValidString(note)||len(note)>MaxNoteBytes{return Screenshot{},fmt.Errorf("Комментарий должен быть UTF-8 и не длиннее 32 KiB")}
	root,err:=os.OpenRoot(a.settings.LibraryDirectory);if err!=nil{return Screenshot{},err};defer root.Close()
	record,err:=loadScreenshot(root,id);if err!=nil{return Screenshot{},err}
	if record.MetadataDigest!=expectedDigest{return Screenshot{},ErrRecordChanged}
	content,err:=readRegular(root,id+".png.json",maxMetadataBytes);if err!=nil{return Screenshot{},err}
	var fields map[string]json.RawMessage
	if err:=decodeJSON(content,&fields);err!=nil{return Screenshot{},err}
	set:=func(key string,value interface{})error{encoded,err:=json.Marshal(value);if err==nil{fields[key]=encoded};return err}
	if err:=set("note",note);err!=nil{return Screenshot{},err}
	set("metadata_revision",record.MetadataRevision+1)
	set("image_revision",record.ImageRevision)
	set("updated_at",time.Now().UTC().Format(time.RFC3339Nano))
	delete(fields,"metadata_digest") // computed wire field, never an on-disk hash
	updated,err:=encodeJSON(fields);if err!=nil{return Screenshot{},err}
	if int64(len(updated))>maxMetadataBytes{return Screenshot{},fmt.Errorf("Метаданные превышают лимит 1 MiB")}
	name,err:=writeTemporary(root,updated);if err!=nil{return Screenshot{},err};defer root.Remove(name)
	current,err:=readRegular(root,id+".png.json",maxMetadataBytes);if err!=nil{return Screenshot{},err}
	digest:=sha256.Sum256(current)
	if hex.EncodeToString(digest[:])!=expectedDigest{return Screenshot{},ErrRecordChanged}
	if err:=root.Rename(name,id+".png.json");err!=nil{return Screenshot{},fmt.Errorf("Комментарий не сохранён: %w",err)}
	return loadScreenshot(root,id)
}

// RecoverOrphan copies an explicitly selected orphan into a fresh canonical
// pair. It never removes the original or trusts a path supplied by a sidecar.
func(a *App)RecoverOrphan(filename,note string)(Screenshot,error){
	id:=strings.TrimSuffix(filename,".png")
	if !validID(id)||filename!=id+".png"{return Screenshot{},fmt.Errorf("Неверный PNG для восстановления")}
	a.mu.Lock()
	root,err:=os.OpenRoot(a.settings.LibraryDirectory)
	if err!=nil{a.mu.Unlock();return Screenshot{},err}
	if _,err:=root.Lstat(filename+".json");!errors.Is(err,os.ErrNotExist){root.Close();a.mu.Unlock();return Screenshot{},fmt.Errorf("У этого PNG уже есть метаданные; исходная запись не изменена")}
	content,err:=readRegular(root,filename,MaxPNGBytes)
	root.Close();a.mu.Unlock()
	if err!=nil{return Screenshot{},err}
	return a.ImportPNGData(content,note)
}
