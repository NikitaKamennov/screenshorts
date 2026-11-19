package core

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"time"
	"unicode/utf8"
)

// PreparePNG reads only the selected source, checks limits before decoding,
// and emits a new PNG without copying source metadata chunks. It does not save
// anything. Its result can be kept in memory for a stable editor preview.
func PreparePNG(source string) ([]byte, error) {
	if source == "" {
		return nil, errors.New("выбери PNG-файл для импорта")
	}
	info, err := os.Stat(source)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть выбранный PNG: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("для импорта нужен обычный PNG-файл")
	}
	file, err := os.Open(source)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть выбранный PNG: %w", err)
	}
	content, readErr := readLimitedFile(file, MaxPNGBytes)
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, fmt.Errorf("не удалось прочитать PNG (лимит 20 MiB): %w", err)
	}
	return PreparePNGData(content)
}

// PreparePNGData applies the same limits to an in-memory native capture/editor
// result. Unsaved capture pixels never need a disk file.
func PreparePNGData(content []byte) ([]byte, error) {
	if int64(len(content)) > MaxPNGBytes { return nil, errors.New("PNG превышает лимит 20 MiB") }
	config, err := png.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("выбранный файл не является корректным PNG: %w", err)
	}
	if err := checkDimensions(config.Width, config.Height); err != nil {
		return nil, err
	}
	decoded, err := png.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("не удалось декодировать PNG: %w", err)
	}
	if decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return nil, errors.New("размеры PNG изменились во время декодирования")
	}
	if palette,ok:=decoded.(*image.Paletted);ok{
		flat:=image.NewNRGBA(palette.Bounds())
		for y:=palette.Bounds().Min.Y;y<palette.Bounds().Max.Y;y++{for x:=palette.Bounds().Min.X;x<palette.Bounds().Max.X;x++{flat.Set(x,y,palette.At(x,y))}}
		decoded=flat
	}
	scrubTransparentPixels(decoded)
	buffer := &limitedPNGBuffer{limit: MaxPNGBytes}
	if err := png.Encode(buffer, decoded); err != nil {
		return nil, fmt.Errorf("не удалось подготовить PNG (итоговый лимит 20 MiB): %w", err)
	}
	return buffer.Bytes(), nil
}

func (a *App) ImportPNG(source, note string) (Screenshot, error) {
	content, err := PreparePNG(source)
	if err != nil { return Screenshot{}, err }
	return a.ImportPNGData(content, note)
}

func (a *App) ImportPNGData(data []byte, note string) (Screenshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !utf8.ValidString(note) {
		return Screenshot{}, errors.New("заметка содержит некорректный текст")
	}
	if len(note) > MaxNoteBytes {
		return Screenshot{}, errors.New("заметка превышает лимит 32 KiB")
	}
	content, err := PreparePNGData(data)
	if err != nil {
		return Screenshot{}, err
	}
	config, err := png.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return Screenshot{}, fmt.Errorf("не удалось прочитать подготовленный PNG: %w", err)
	}
	root, err := os.OpenRoot(a.settings.LibraryDirectory)
	if err != nil { return Screenshot{}, fmt.Errorf("не удалось открыть библиотеку: %w", err) }
	defer root.Close()
	createdAt := time.Now()
	id, err := availableScreenshotID(root, createdAt)
	if err != nil {
		return Screenshot{}, fmt.Errorf("не удалось создать идентификатор: %w", err)
	}
	hash := sha256.Sum256(content)
	screenshot := Screenshot{
		Schema: Schema,
		ID: id,
		ImageFile: id + ".png",
		CreatedAt: createdAt.UTC().Format(time.RFC3339Nano),
		Width: config.Width,
		Height: config.Height,
		Note: note,
		Tags: []string{},
		OCR: OCRResult{Status: "not_requested",InputSHA256:hex.EncodeToString(hash[:])},
		ImageSHA256: hex.EncodeToString(hash[:]),
		MetadataRevision:1,
		ImageRevision:1,
	}
	metadata, err := encodeJSON(screenshot)
	if err != nil {
		return Screenshot{}, fmt.Errorf("не удалось подготовить метаданные: %w", err)
	}
	imageTemp, err := writeTemporary(root, content)
	if err != nil {
		return Screenshot{}, fmt.Errorf("не удалось записать временный PNG: %w", err)
	}
	defer root.Remove(imageTemp)
	metadataTemp, err := writeTemporary(root, metadata)
	if err != nil {
		return Screenshot{}, fmt.Errorf("не удалось записать временные метаданные: %w", err)
	}
	defer root.Remove(metadataTemp)
	// Hard links atomically publish a complete file and fail if the target
	// exists. Rename alone would overwrite another file on Unix.
	if err := root.Link(imageTemp, screenshot.ImageFile); err != nil {
		return Screenshot{}, fmt.Errorf("PNG не сохранён; требуется библиотека на файловой системе с поддержкой жёстких ссылок и свободное имя: %w", err)
	}
	if err := root.Link(metadataTemp, screenshot.ImageFile + ".json"); err != nil {
		rollbackErr := root.Remove(screenshot.ImageFile)
		if rollbackErr != nil {
			return Screenshot{}, fmt.Errorf("метаданные не сохранены; не удалось убрать незавершённый PNG %s: %w", screenshot.ImageFile, errors.Join(err, rollbackErr))
		}
		return Screenshot{}, fmt.Errorf("метаданные не сохранены, импорт отменён: %w", err)
	}
	digest:=sha256.Sum256(metadata);screenshot.MetadataDigest=hex.EncodeToString(digest[:])
	return screenshot, nil
}

func checkDimensions(width, height int) error {
	if width <= 0 || height <= 0 || width > MaxPixels || height > MaxPixels || int64(width) * int64(height) > MaxPixels {
		return errors.New("PNG должен содержать от 1 до 16 000 000 пикселей")
	}
	return nil
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func writeTemporary(root *os.Root, content []byte) (string, error) {
	for attempt := 0; attempt < 4; attempt++ {
		id, err := randomID()
		if err != nil {
			return "", err
		}
		name := ".skrinpolka-" + id + ".tmp"
		file, err := root.OpenFile(name, os.O_WRONLY | os.O_CREATE | os.O_EXCL, 0600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		written, writeErr := file.Write(content)
		if writeErr == nil && written != len(content) {
			writeErr = io.ErrShortWrite
		}
		var syncErr error
		if writeErr == nil {
			syncErr = file.Sync()
		}
		closeErr := file.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			removeErr := root.Remove(name)
			return "", errors.Join(err, removeErr)
		}
		return name, nil
	}
	return "", errors.New("не удалось подобрать свободное имя временного файла")
}

type limitedPNGBuffer struct {
	bytes.Buffer
	limit int64
}

func (b *limitedPNGBuffer) Write(content []byte) (int, error) {
	if int64(len(content)) > b.limit - int64(b.Len()) {
		return 0, errors.New("перекодированный PNG превышает лимит 20 MiB")
	}
	return b.Buffer.Write(content)
}

// Remove invisible RGB from fully transparent pixels without changing the
// bit depth of decoded PNGs. Other PNG decoder result types are opaque.
func scrubTransparentPixels(decoded image.Image) {
	switch typed := decoded.(type) {
	case *image.NRGBA:
		for offset := 0; offset + 3 < len(typed.Pix); offset += 4 {
			if typed.Pix[offset + 3] == 0 {
				clear(typed.Pix[offset:offset + 3])
			}
		}
	case *image.NRGBA64:
		for offset := 0; offset + 7 < len(typed.Pix); offset += 8 {
			if typed.Pix[offset + 6] == 0 && typed.Pix[offset + 7] == 0 {
				clear(typed.Pix[offset:offset + 6])
			}
		}
	case *image.Paletted:
		for index, entry := range typed.Palette {
			_, _, _, alpha := entry.RGBA()
			if alpha == 0 {
				typed.Palette[index] = color.NRGBA{}
			}
		}
	}
}
