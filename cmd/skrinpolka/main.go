package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"skrinpolka/frontend"
	"skrinpolka/internal/analysis"
	"skrinpolka/internal/core"
	"skrinpolka/internal/platform"
	"skrinpolka/internal/sandbox"
)

// Desktop is the native bridge for the library, editor, capture and OCR jobs.
type Desktop struct {
	core      *core.App
	dataRoot  string
	contextMu sync.RWMutex
	ctx       context.Context
	operation sync.Mutex
	dialog    sync.Mutex
	selection sync.Mutex
	pending   *pendingPNG
	nativeMu sync.RWMutex
	windowMu sync.Mutex
	native platform.Service
	nativeProblem string
	cancel context.CancelFunc
	capturing atomic.Bool
	quitting atomic.Bool
	visible atomic.Bool
	nativeStarted atomic.Bool
	ocrService *analysis.OCRService
	backgroundService *analysis.BackgroundService
	jobs sync.WaitGroup
	jobsGate sync.Mutex
	sessionID string
	recordJobs map[string]*pendingPNG
	completedCapture *savedCapture
}

func (d *Desktop) startup(ctx context.Context) {
	ctx, d.cancel = context.WithCancel(ctx)
	d.contextMu.Lock()
	d.ctx = ctx
	d.contextMu.Unlock()
	d.visible.Store(true)
}

func (d *Desktop) shutdown(context.Context) {
	d.jobsGate.Lock()
	d.quitting.Store(true)
	if d.cancel != nil { d.cancel() }
	d.jobsGate.Unlock()
	d.jobs.Wait()
	d.nativeMu.Lock()
	service := d.native
	d.native = nil
	d.nativeMu.Unlock()
	if service != nil {
		if err := service.Close(); err != nil { fmt.Fprintln(os.Stderr, "ScreenShooter Pro: завершение системного сервиса:", err) }
	}
	d.selection.Lock()
	d.pending = nil
	d.selection.Unlock()
}

func (d *Desktop) nativeContext() (context.Context, error) {
	d.contextMu.RLock()
	defer d.contextMu.RUnlock()
	if d.ctx == nil {
		return nil, fmt.Errorf("Окно приложения ещё не готово")
	}
	return d.ctx, nil
}

func (d *Desktop) show() {
	d.windowMu.Lock()
	defer d.windowMu.Unlock()
	ctx, err := d.nativeContext()
	if err == nil && ctx.Err() == nil && !d.quitting.Load() {
		wruntime.WindowUnminimise(ctx)
		wruntime.WindowShow(ctx)
		if !d.visible.Load() { wruntime.WindowCenter(ctx) }
		d.visible.Store(true)
	}
}

// Serialize hiding with tray-loss recovery. A late capability callback must
// show the window after hiding, rather than get overwritten by startup.
func (d *Desktop) hideToTray() bool {
	d.windowMu.Lock()
	defer d.windowMu.Unlock()
	ctx, err := d.nativeContext()
	if err != nil || ctx.Err() != nil || d.quitting.Load() { return false }
	service := d.nativeService()
	if service == nil || !service.Capabilities().Tray.Available { return false }
	wruntime.WindowHide(ctx)
	d.visible.Store(false)
	return true
}

func (d *Desktop) showCentered() {
	d.show()
	if ctx, err := d.nativeContext(); err == nil && ctx.Err() == nil && !d.quitting.Load() {
		wruntime.WindowCenter(ctx)
	}
}

func (d *Desktop) GetState() (core.Snapshot, error) {
	snapshot,err:=d.core.Snapshot();if err!=nil{return snapshot,err}
	d.selection.Lock()
	for index,record:=range snapshot.Screenshots{if job:=d.recordJobs[snapshot.Settings.LibraryDirectory+"|"+record.ID];job!=nil&&job.ocr.InputSHA256==record.ImageSHA256{snapshot.Screenshots[index].OCR=job.ocr}}
	d.selection.Unlock()
	return snapshot,nil
}

func (d *Desktop) GetSettings() core.Settings {
	settings:=d.core.CurrentSettings()
	if !settings.SetupComplete && settings.CaptureShortcut=="" {settings.CaptureShortcut=platform.DefaultShortcut}
	return settings
}

func (d *Desktop) GetCapabilities() platform.Capabilities {
	d.nativeMu.RLock(); defer d.nativeMu.RUnlock()
	if d.native != nil { return d.native.Capabilities() }
	if d.nativeProblem!="" { return platform.Unavailable(d.nativeProblem) }
	return platform.Detect()
}

func (d *Desktop) SearchScreenshots(query string) ([]core.Screenshot, error) {
	return d.core.Search(query)
}

func (d *Desktop) UpdateNote(id,expectedDigest,note string)(core.Screenshot,error){
	d.operation.Lock();defer d.operation.Unlock()
	return d.core.UpdateNote(id,expectedDigest,note)
}

func(d *Desktop)RecoverOrphan(filename,note string)(core.Screenshot,error){
	d.operation.Lock();defer d.operation.Unlock()
	record,err:=d.core.RecoverOrphan(filename,note)
	if err!=nil{return core.Screenshot{},err}
	return d.ensureSavedOCRLocked(record),nil
}

func (d *Desktop) SaveSettings(settings core.Settings) (core.Settings, error) {
	d.operation.Lock()
	defer d.operation.Unlock()
	previous := d.core.CurrentSettings()
	settings.Shelf = previous.Shelf
	// Layout has its own narrow save operation; preserve the most recent value.
	settings.EditorPanelPosition, settings.EditorToolOrder = previous.EditorPanelPosition, previous.EditorToolOrder
	settings.EditorPanelWidth, settings.EditorPanelHeight = previous.EditorPanelWidth, previous.EditorPanelHeight
	settings.EditorWidth, settings.EditorHeight = previous.EditorWidth, previous.EditorHeight
	settings.SilentRegion = previous.SilentRegion
	shortcuts, err := platform.NormalizeShortcuts(platform.Shortcuts{Area: settings.CaptureShortcut, Screen: settings.ScreenShortcut, Silent: settings.SilentShortcut, SilentArea:settings.SilentAreaShortcut})
	if err != nil { return core.Settings{}, err }
	settings.CaptureShortcut, settings.ScreenShortcut = shortcuts.Area, shortcuts.Screen
	settings.SilentShortcut = shortcuts.Silent
	settings.SilentAreaShortcut = shortcuts.SilentArea
	if settings.SilentAreaShortcut!=""&&settings.SilentRegion==nil{return core.Settings{},fmt.Errorf("Сначала нажми «Задать область тихого скриншота»")}
	service := d.nativeService()
	changed := settings.CaptureShortcut != previous.CaptureShortcut || settings.ScreenShortcut != previous.ScreenShortcut || settings.SilentShortcut != previous.SilentShortcut || settings.SilentAreaShortcut != previous.SilentAreaShortcut
	if changed {
		if service == nil { return core.Settings{}, fmt.Errorf("Системные адаптеры ещё запускаются") }
		ctx,err:=d.nativeContext();if err!=nil{return core.Settings{},err}
		if _,err=service.SetShortcuts(ctx,shortcuts);err!=nil{return core.Settings{},err}
	}
	settings.SetupComplete = true
	saved,err := d.core.SaveSettings(settings)
	if err != nil && service != nil && changed {
		ctx,_:=d.nativeContext()
		rollback := platform.Shortcuts{Area: previous.CaptureShortcut, Screen: previous.ScreenShortcut, Silent: previous.SilentShortcut, SilentArea:previous.SilentAreaShortcut}
		if !service.Capabilities().SilentCapture.Available { rollback.Silent = "";rollback.SilentArea="" }
		if _, rollbackErr := service.SetShortcuts(ctx, rollback); rollbackErr != nil {
			return core.Settings{}, fmt.Errorf("%v; восстановить прежние комбинации не удалось: %v", err, rollbackErr)
		}
	}
	if err == nil { d.applyNativeTheme(saved.Theme) }
	return saved,err
}

func (d *Desktop) SaveAppearance(value core.Appearance) (core.Appearance, error) {
	d.operation.Lock(); defer d.operation.Unlock()
	saved, err := d.core.SaveAppearance(value)
	if err == nil { d.applyNativeTheme(saved.Theme) }
	return saved, err
}

func (d *Desktop) applyNativeTheme(theme string) {
	ctx, err := d.nativeContext(); if err != nil || ctx.Err() != nil { return }
	if theme == "dark" {
		wruntime.WindowSetBackgroundColour(ctx, 19, 24, 31, 255)
		wruntime.WindowSetDarkTheme(ctx)
	} else {
		wruntime.WindowSetBackgroundColour(ctx, 247, 248, 244, 255)
		wruntime.WindowSetLightTheme(ctx)
	}
}

func (d *Desktop) SetWorkspaceMaximised(expanded bool) {
	ctx, err := d.nativeContext(); if err != nil { return }
	if expanded { wruntime.WindowMaximise(ctx) } else { wruntime.WindowUnmaximise(ctx) }
}

func (d *Desktop) SaveEditorLayout(layout core.EditorLayout) (core.EditorLayout, error) {
	d.operation.Lock()
	defer d.operation.Unlock()
	return d.core.SaveEditorLayout(layout)
}

func (d *Desktop) SelectLibraryDirectory() (string, error) {
	d.dialog.Lock()
	defer d.dialog.Unlock()
	ctx, err := d.nativeContext()
	if err != nil {
		return "", err
	}
	defaultDirectory := d.core.CurrentSettings().LibraryDirectory
	if info, err := os.Stat(defaultDirectory); err != nil || !info.IsDir() {
		defaultDirectory = ""
	}
	return wruntime.OpenDirectoryDialog(ctx, wruntime.OpenDialogOptions{
		Title: "Выбери папку библиотеки ScreenShooter Pro",
		DefaultDirectory: defaultDirectory,
	})
}

func defaultDataRoot() (string, error) {
	if runtime.GOOS == "windows" {
		if root := os.Getenv("LOCALAPPDATA"); root != "" {
			return filepath.Join(root, "Skrinpolka"), nil
		}
	}
	if runtime.GOOS == "linux" {
		if root := os.Getenv("XDG_DATA_HOME"); root != "" && filepath.IsAbs(root) {
			return filepath.Join(root, "skrinpolka"), nil
		}
		homeDirectory, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDirectory, ".local", "share", "skrinpolka"), nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "Skrinpolka"), nil
}

func run() error {
	platform.ConfigureProcess()
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		return fmt.Errorf("Каркас подготовлен для Windows и Linux")
	}
	dataFlag := flag.String("data-root", "", "Отдельный каталог настроек и библиотеки")
	runtimeFlag:=flag.String("runtime-config","","Конфигурация локальных защищённых workers")
	flag.Parse()
	dataRoot := *dataFlag
	var err error
	if dataRoot == "" {
		dataRoot, err = defaultDataRoot()
		if err != nil {
			return err
		}
	}
	dataRoot, err = filepath.Abs(dataRoot)
	if err != nil {
		return err
	}
	library, err := core.New(dataRoot)
	if err != nil {
		return err
	}
	assets, err := frontend.Files()
	if err != nil {
		return err
	}
	desktop := &Desktop{core: library, dataRoot: dataRoot}
	sessionID,err:=newSessionID();if err!=nil{return err};desktop.sessionID=sessionID;desktop.recordJobs=map[string]*pendingPNG{}
	runtimeConfig:=*runtimeFlag
	if runtimeConfig==""{executable,err:=os.Executable();if err!=nil{return err};runtimeConfig=filepath.Join(filepath.Dir(executable),"config","runtime-"+runtime.GOOS+".json")}
	workerQueue:=analysis.NewQueue()
	desktop.ocrService=analysis.NewOCR(runtimeConfig,filepath.Join(dataRoot,"work"),workerQueue)
	desktop.backgroundService=analysis.NewBackground(runtimeConfig,filepath.Join(dataRoot,"work"),workerQueue)
	background := options.NewRGB(247, 248, 244)
	if library.CurrentSettings().Theme == "dark" { background = options.NewRGB(19, 24, 31) }
	windowOptions,err:=nativeWindowOptions(dataRoot)
	if err!=nil{return err}
	return wails.Run(&options.App{
		Title: "ScreenShooter Pro",
		Width: 1320, Height: 900, MinWidth: 980, MinHeight: 680,
		StartHidden: false, HideWindowOnClose: false,
		BackgroundColour: background,
		AssetServer: &assetserver.Options{Assets: assets},
		Bind: []interface{}{desktop},
		OnStartup: desktop.startup,
		OnDomReady: desktop.domReady,
		OnBeforeClose: desktop.beforeClose,
		OnShutdown: desktop.shutdown,
		EnableDefaultContextMenu: false,
		EnableFraudulentWebsiteDetection: false,
		DragAndDrop: &options.DragAndDrop{DisableWebViewDrop: true},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "skrinpolka-local-desktop-v1",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { desktop.show() },
		},
		Windows: windowOptions,
	})
}

func main() {
	if handled, code := sandbox.WorkerGuard(os.Args[1:]); handled {
		os.Exit(code)
	}
	if err := run(); err != nil {
		reportStartupError(err)
		os.Exit(1)
	}
}
