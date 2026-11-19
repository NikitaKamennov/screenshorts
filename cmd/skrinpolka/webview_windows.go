//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options/windows"
	syswindows "golang.org/x/sys/windows"
)

func nativeWindowOptions(dataRoot string) (*windows.Options, error) {
	options := &windows.Options{WindowClassName:"Skrinpolka", WebviewUserDataPath:filepath.Join(dataRoot,"webview")}
	executable, err := os.Executable()
	if err != nil { return nil, err }
	root := filepath.Dir(executable)
	marker := filepath.Join(root,"config","windows-bundle.json")
	file, err := os.Open(marker)
	if errors.Is(err,os.ErrNotExist) && distributionMode != "bundle" { return options,nil }
	if err != nil { return nil,fmt.Errorf("Неполный комплект ScreenShorts Pro: скопируй всю папку программы вместе с config, tools и runtime: %w",err) }
	content, readErr := io.ReadAll(io.LimitReader(file,8193))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(content)>8192 { return nil,fmt.Errorf("Не удалось прочитать описание Windows-комплекта") }
	var descriptor struct { Schema string `json:"schema"`; Architecture string `json:"architecture"`; WebviewDirectory string `json:"webview2_directory"` }
	if json.Unmarshal(content,&descriptor)!=nil || descriptor.Schema!="screenshorts-windows-bundle-v1" || descriptor.Architecture!="amd64" || descriptor.WebviewDirectory!="../tools/webview2" {
		return nil,fmt.Errorf("Неподдержанное описание Windows-комплекта")
	}
	for _, relative := range []string{"config/runtime-windows.json","tools/webview2/msedgewebview2.exe","tools/webview2/msedge.dll","tools/tesseract/tesseract.exe","tools/tesseract/tessdata/rus.traineddata","tools/tesseract/tessdata/eng.traineddata","runtime/python/python.exe","runtime/python/python313.zip","runtime/python/python313._pth","workers/background/remove_background.py","models/background/u2netp.onnx"} {
		path := filepath.Join(root,filepath.FromSlash(relative))
		actual, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr != nil { return nil,fmt.Errorf("В комплекте нет %s; распакуй всю папку программы заново",relative) }
		rel, relErr := filepath.Rel(root,actual)
		if relErr!=nil || rel==".." || len(rel)>3 && rel[:3]==".."+string(filepath.Separator) { return nil,fmt.Errorf("Путь компонента выходит за папку программы: %s",relative) }
		if info, statErr := os.Stat(actual); statErr!=nil || !info.Mode().IsRegular() { return nil,fmt.Errorf("Недопустимый файл компонента: %s",relative) }
	}
	options.WebviewBrowserPath = filepath.Join(root,"tools","webview2")
	version := syswindows.RtlGetVersion()
	if version.MajorVersion<10 || version.MajorVersion==10 && version.BuildNumber<19045 { return nil,fmt.Errorf("Этот комплект рассчитан на Windows 10 22H2 или Windows 11 x64") }
	if version.BuildNumber<22000 {
		// Microsoft requires these read-only runtime ACLs for unpackaged Fixed
		// Version renderers on Windows 10. Only this bundle's own folder is used.
		ctx,cancel := context.WithTimeout(context.Background(),15*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx,filepath.Join(os.Getenv("SystemRoot"),"System32","icacls.exe"),options.WebviewBrowserPath,"/grant","*S-1-15-2-2:(OI)(CI)(RX)","*S-1-15-2-1:(OI)(CI)(RX)")
		command.SysProcAttr = &syscall.SysProcAttr{HideWindow:true,CreationFlags:0x08000000}
		command.WaitDelay=time.Second
		if err:=command.Run();err!=nil{return nil,fmt.Errorf("Не удалось подготовить доступ WebView2 к собственной папке runtime: %w",err)}
	}
	return options,nil
}
