package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func (a *App) Search(query string) ([]Screenshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	screenshots, _, err := a.listScreenshots()
	if err != nil {
		return nil, err
	}
	words := strings.Fields(strings.ToLower(query))
	matched := make([]Screenshot, 0, len(screenshots))
	for _, screenshot := range screenshots {
		parts := []string{screenshot.Note}
		if screenshot.OCR.InputSHA256==screenshot.ImageSHA256{parts=append(parts,EffectiveOCR(screenshot.OCR))}
		parts = append(parts, screenshot.Tags...)
		text := strings.ToLower(strings.Join(parts, "\n"))
		match := true
		for _, word := range words {
			if !strings.Contains(text, word) {
				match = false
				break
			}
		}
		if match {
			matched = append(matched, screenshot)
		}
	}
	return matched, nil
}

func (a *App) listScreenshots() ([]Screenshot, []LibraryWarning, error) {
	root, err := os.OpenRoot(a.settings.LibraryDirectory)
	if err != nil {
		return nil, nil, fmt.Errorf("не удалось открыть библиотеку: %w", err)
	}
	defer root.Close()
	directory, err := root.Open(".")
	if err != nil {
		return nil, nil, fmt.Errorf("не удалось прочитать библиотеку: %w", err)
	}
	entries, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, nil, fmt.Errorf("не удалось прочитать список файлов: %w", err)
	}
	screenshots := make([]Screenshot, 0)
	warnings := make([]LibraryWarning,0)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name,".skrinpolka-") && strings.HasSuffix(name,".tmp") {
			warnings=append(warnings,LibraryWarning{File:name,Kind:"temporary",Message:"Остался временный файл. Он не удалён автоматически."});continue
		}
		if strings.HasSuffix(name,".png") && validID(strings.TrimSuffix(name,".png")) {
			if _,err:=root.Lstat(name+".json");errors.Is(err,os.ErrNotExist){warnings=append(warnings,LibraryWarning{File:name,Kind:"orphan_png",Message:"PNG без метаданных: можно импортировать отдельную копию."})}
		}
		if !strings.HasSuffix(name, ".png.json") {
			continue
		}
		id := strings.TrimSuffix(name, ".png.json")
		if !validID(id) {
			continue
		}
		screenshot, err := loadScreenshot(root, id)
		if err != nil {
			warnings=append(warnings,LibraryWarning{File:name,Kind:"invalid_record",Message:err.Error()});continue
		}
		screenshots = append(screenshots, screenshot)
	}
	sort.Slice(screenshots, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339Nano, screenshots[i].CreatedAt)
		right, _ := time.Parse(time.RFC3339Nano, screenshots[j].CreatedAt)
		if left.Equal(right) {
			return screenshots[i].ID < screenshots[j].ID
		}
		return left.After(right)
	})
	return screenshots, warnings, nil
}

func loadScreenshot(root *os.Root, id string) (Screenshot, error) {
	if !validID(id) {
		return Screenshot{}, errors.New("некорректный идентификатор скриншота")
	}
	content, err := readRegular(root, id + ".png.json", maxMetadataBytes)
	if err != nil {
		return Screenshot{}, err
	}
	var screenshot Screenshot
	if err := decodeJSON(content, &screenshot); err != nil {
		return Screenshot{}, fmt.Errorf("повреждён JSON метаданных: %w", err)
	}
	if screenshot.Schema != Schema {
		return Screenshot{}, errors.New("неподдерживаемая схема метаданных")
	}
	if screenshot.ID != id || screenshot.ImageFile != id + ".png" {
		return Screenshot{}, errors.New("имя картинки и идентификатор в метаданных не совпадают")
	}
	if err := checkDimensions(screenshot.Width, screenshot.Height); err != nil {
		return Screenshot{}, fmt.Errorf("некорректные размеры в метаданных: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, screenshot.CreatedAt); err != nil {
		return Screenshot{}, errors.New("некорректная дата создания в метаданных")
	}
	if len(screenshot.ImageSHA256) != 64 {
		return Screenshot{}, errors.New("отсутствует корректный SHA-256 картинки")
	}
	if _, err := hex.DecodeString(screenshot.ImageSHA256); err != nil {
		return Screenshot{}, errors.New("некорректный SHA-256 картинки")
	}
	if !utf8.ValidString(screenshot.Note) || len(screenshot.Note) > MaxNoteBytes {
		return Screenshot{}, errors.New("заметка повреждена или превышает лимит 32 KiB")
	}
	info, err := root.Lstat(screenshot.ImageFile)
	if err != nil {
		return Screenshot{}, fmt.Errorf("картинка записи недоступна: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > MaxPNGBytes {
		return Screenshot{}, errors.New("картинка должна быть обычным PNG-файлом до 20 MiB")
	}
	screenshot.ImageBytes = info.Size()
	if screenshot.Tags == nil {
		screenshot.Tags = []string{}
	}
	if screenshot.MetadataRevision==0{screenshot.MetadataRevision=1}
	if screenshot.ImageRevision==0{screenshot.ImageRevision=1}
	digest:=sha256.Sum256(content);screenshot.MetadataDigest=hex.EncodeToString(digest[:])
	return screenshot, nil
}

// ReadPNG accepts only an application ID, never a path from the caller or JSON.
// It checks the final bytes against the authoritative sidecar before returning.
func (a *App) ReadPNG(id string) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return readPNGAt(a.settings.LibraryDirectory,id)
}

func(a *App)ReadPNGAt(library,id string)([]byte,error){
	a.mu.Lock();defer a.mu.Unlock();return readPNGAt(library,id)
}

func readPNGAt(library,id string)([]byte,error){
	if !validID(id) {
		return nil, errors.New("некорректный идентификатор скриншота")
	}
	root, err := os.OpenRoot(library)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть библиотеку: %w", err)
	}
	defer root.Close()
	screenshot, err := loadScreenshot(root, id)
	if err != nil {
		return nil, fmt.Errorf("скриншот недоступен: %w", err)
	}
	content, err := readRegular(root, id + ".png", MaxPNGBytes)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать скриншот: %w", err)
	}
	hash := sha256.Sum256(content)
	if !strings.EqualFold(hex.EncodeToString(hash[:]), screenshot.ImageSHA256) {
		return nil, errors.New("PNG изменён: SHA-256 не совпадает с метаданными")
	}
	config, err := png.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("файл записи не является корректным PNG: %w", err)
	}
	if err := checkDimensions(config.Width, config.Height); err != nil {
		return nil, err
	}
	if config.Width != screenshot.Width || config.Height != screenshot.Height {
		return nil, errors.New("размер PNG не совпадает с метаданными")
	}
	return content, nil
}

func validID(id string) bool {
	if validScreenshotName(id) { return true }
	if len(id) != 32 {
		return false
	}
	for _, char := range id {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}
