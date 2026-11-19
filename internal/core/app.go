package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	Schema = "skrinpolka-screenshot-v1"
	MaxPNGBytes int64 = 20 << 20
	MaxPixels = 16_000_000
	MaxNoteBytes = 32 << 10
	maxMetadataBytes int64 = 1 << 20
	maxSettingsBytes int64 = 64 << 10
)

type Settings struct {
	Shelf ShelfPreferences `json:"shelf"`
	LibraryDirectory string `json:"library_directory"`
	CaptureShortcut string `json:"capture_shortcut"`
	ScreenShortcut string `json:"screen_shortcut"`
	SilentShortcut string `json:"silent_shortcut"`
	SilentAreaShortcut string `json:"silent_area_shortcut"`
	SilentRegion *ScreenRegion `json:"silent_region,omitempty"`
	EditorPanelPosition string `json:"editor_panel_position"`
	EditorToolOrder []string `json:"editor_tool_order"`
	EditorPanelWidth int `json:"editor_panel_width"`
	EditorPanelHeight int `json:"editor_panel_height"`
	EditorWidth int `json:"editor_width"`
	EditorHeight int `json:"editor_height"`
	Theme string `json:"theme"`
	AccentColor string `json:"accent_color"`
	StartInTray bool `json:"start_in_tray"`
	AutoIndex bool `json:"auto_index"`
	SetupComplete bool `json:"setup_complete"`
}

type OCRResult struct {
	Status string `json:"status"`
	RawText string `json:"raw_text"`
	CorrectedText string `json:"corrected_text"`
	HasCorrection bool `json:"has_correction,omitempty"`
	InputSHA256 string `json:"input_sha256,omitempty"`
	Engine string `json:"engine,omitempty"`
	ModelRevision string `json:"model_revision,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	Error string `json:"error,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

type Screenshot struct {
	ImageBytes int64 `json:"image_bytes,omitempty"` // Computed from PNG; not required in portable metadata.
	Schema string `json:"schema"`
	ID string `json:"id"`
	ImageFile string `json:"image_file"`
	CreatedAt string `json:"created_at"`
	Width int `json:"width"`
	Height int `json:"height"`
	Note string `json:"note"`
	Tags []string `json:"tags"`
	OCR OCRResult `json:"ocr"`
	ImageSHA256 string `json:"image_sha256"`
	MetadataRevision uint64 `json:"metadata_revision,omitempty"`
	ImageRevision uint64 `json:"image_revision,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
	MetadataDigest string `json:"metadata_digest,omitempty"`
}

type LibraryWarning struct {
	File string `json:"file"`
	Kind string `json:"kind"`
	Message string `json:"message"`
}

type Snapshot struct {
	Settings Settings `json:"settings"`
	Screenshots []Screenshot `json:"screenshots"`
	Warnings []LibraryWarning `json:"warnings"`
}

// App serializes its own operations. A library is intended for one app process.
// Sidecar JSON files are authoritative; the OCR worker merges only its fields.
// There is no database index yet.
type App struct {
	mu sync.Mutex
	dataRoot string
	settings Settings
}

func New(dataRoot string) (*App, error) {
	if strings.TrimSpace(dataRoot) == "" {
		return nil, errors.New("не указан каталог данных приложения")
	}
	absolute, err := filepath.Abs(dataRoot)
	if err != nil {
		return nil, fmt.Errorf("не удалось определить каталог данных: %w", err)
	}
	if err := os.MkdirAll(absolute, 0700); err != nil {
		return nil, fmt.Errorf("не удалось создать каталог данных: %w", err)
	}
	app := &App{dataRoot: filepath.Clean(absolute)}
	root, err := os.OpenRoot(app.dataRoot)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть каталог данных: %w", err)
	}
	defer root.Close()
	content, err := readRegular(root, "settings.json", maxSettingsBytes)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return nil, fmt.Errorf("не удалось прочитать настройки: %w", err)
	}
	settings := Settings{LibraryDirectory: filepath.Join(app.dataRoot, "library")}
	if !missing {
		if err := decodeJSON(content, &settings); err != nil {
			return nil, fmt.Errorf("файл настроек повреждён: %w", err)
		}
	}
	settings, err = app.normalizeSettings(settings)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(settings.LibraryDirectory, 0700); err != nil {
		return nil, fmt.Errorf("не удалось открыть или создать библиотеку: %w", err)
	}
	if missing {
		if err := saveSettingsFile(root, settings); err != nil {
			return nil, err
		}
	}
	app.settings = settings
	return app, nil
}

func (a *App) Snapshot() (Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	screenshots, warnings, err := a.listScreenshots()
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Settings: a.settings, Screenshots: screenshots, Warnings:warnings}, nil
}

// CurrentSettings does not scan the library, so its folder can be changed even
// if a record in the current folder has become unreadable.
func (a *App) CurrentSettings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

// SaveSettings stores preferences; the desktop bridge applies native shortcuts.
func (a *App) SaveSettings(settings Settings) (Settings, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	normalized, err := a.normalizeSettings(settings)
	if err != nil {
		return Settings{}, err
	}
	if err := os.MkdirAll(normalized.LibraryDirectory, 0700); err != nil {
		return Settings{}, fmt.Errorf("не удалось открыть или создать библиотеку: %w", err)
	}
	library, err := os.OpenRoot(normalized.LibraryDirectory)
	if err != nil {
		return Settings{}, fmt.Errorf("не удалось открыть выбранную библиотеку: %w", err)
	}
	if err := library.Close(); err != nil {
		return Settings{}, fmt.Errorf("не удалось закрыть выбранную библиотеку: %w", err)
	}
	root, err := os.OpenRoot(a.dataRoot)
	if err != nil {
		return Settings{}, fmt.Errorf("не удалось открыть каталог настроек: %w", err)
	}
	defer root.Close()
	if err := saveSettingsFile(root, normalized); err != nil {
		return Settings{}, err
	}
	a.settings = normalized
	return normalized, nil
}

func (a *App) normalizeSettings(settings Settings) (Settings, error) {
	if strings.TrimSpace(settings.LibraryDirectory) == "" {
		settings.LibraryDirectory = filepath.Join(a.dataRoot, "library")
	}
	if !utf8.ValidString(settings.LibraryDirectory) || !utf8.ValidString(settings.CaptureShortcut) || !utf8.ValidString(settings.ScreenShortcut) || !utf8.ValidString(settings.SilentShortcut) || !utf8.ValidString(settings.SilentAreaShortcut) {
		return Settings{}, errors.New("настройки содержат некорректный текст")
	}
	absolute, err := filepath.Abs(settings.LibraryDirectory)
	if err != nil {
		return Settings{}, fmt.Errorf("некорректный путь к библиотеке: %w", err)
	}
	settings.LibraryDirectory = filepath.Clean(absolute)
	settings.CaptureShortcut = strings.TrimSpace(settings.CaptureShortcut)
	settings.ScreenShortcut = strings.TrimSpace(settings.ScreenShortcut)
	settings.SilentShortcut = strings.TrimSpace(settings.SilentShortcut)
	settings.SilentAreaShortcut = strings.TrimSpace(settings.SilentAreaShortcut)
	if len(settings.CaptureShortcut) > 128 || len(settings.ScreenShortcut) > 128 || len(settings.SilentShortcut) > 128 || len(settings.SilentAreaShortcut) > 128 {
		return Settings{}, errors.New("описание сочетания клавиш слишком длинное")
	}
	layout, err := normalizeEditorLayout(LayoutFromSettings(settings))
	if err != nil { return Settings{}, err }
	applyEditorLayout(&settings, layout)
	appearance, err := normalizeAppearance(Appearance{Theme: settings.Theme, AccentColor: settings.AccentColor})
	if err != nil { return Settings{}, err }
	settings.Theme, settings.AccentColor = appearance.Theme, appearance.AccentColor
	if settings.SilentRegion!=nil{if err:=settings.SilentRegion.Validate();err!=nil{return Settings{},err}}
	return settings, nil
}

func saveSettingsFile(root *os.Root, settings Settings) error {
	content, err := encodeJSON(settings)
	if err != nil {
		return fmt.Errorf("не удалось подготовить настройки: %w", err)
	}
	name, err := writeTemporary(root, content)
	if err != nil {
		return fmt.Errorf("не удалось записать временные настройки: %w", err)
	}
	defer root.Remove(name)
	if info, err := root.Lstat("settings.json"); err == nil && !info.Mode().IsRegular() {
		return errors.New("настройки не сохранены: settings.json должен быть обычным файлом")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("не удалось проверить файл настроек: %w", err)
	}
	if err := root.Rename(name, "settings.json"); err != nil {
		return fmt.Errorf("не удалось сохранить настройки: %w", err)
	}
	return nil
}

func encodeJSON(value any) ([]byte, error) {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

func decodeJSON(content []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("ожидался один JSON-документ")
	}
	return nil
}

// Root prevents symlinks from escaping the chosen directory. Lstat also
// rejects a symlink in place of the requested file in the normal case.
func readRegular(root *os.Root, name string, limit int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("ожидался обычный файл без символической ссылки")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readLimitedFile(file, limit)
}

func readLimitedFile(file *os.File, limit int64) ([]byte, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("ожидался обычный файл")
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("файл превышает лимит %d байт", limit)
	}
	content, err := io.ReadAll(io.LimitReader(file, limit + 1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("файл превышает лимит %d байт", limit)
	}
	return content, nil
}
