package main

import (
	"fmt"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"skrinpolka/internal/core"
	"skrinpolka/internal/platform"
)

func (d *Desktop) SaveShelfPreferences(value core.ShelfPreferences) (core.ShelfPreferences, error) {
	d.operation.Lock()
	defer d.operation.Unlock()
	return d.core.SaveShelfPreferences(value)
}

func (d *Desktop) GetShelfThumbnail(library, id string) (string, error) {
	d.operation.Lock()
	defer d.operation.Unlock()
	if library != d.core.CurrentSettings().LibraryDirectory {
		return "", fmt.Errorf("Библиотека сменилась")
	}
	data, err := d.core.ReadPNG(id)
	if err != nil {
		return "", err
	}
	return previewDataURLMax(data, 480)
}

func (d *Desktop) TrashScreenshots(value core.ShelfSelection) (core.BatchResult, error) {
	d.operation.Lock()
	defer d.operation.Unlock()
	if value.Library != d.core.CurrentSettings().LibraryDirectory {
		return core.BatchResult{}, fmt.Errorf("Библиотека сменилась")
	}
	if _, err := d.core.ShelfFilePaths(value, false); err != nil {
		return core.BatchResult{}, err
	}
	d.selection.Lock()
	for _, id := range value.IDs {
		key := value.Library + "|" + id
		if job := d.recordJobs[key]; job != nil {
			job.generation++
			if job.ocrCancel != nil {
				job.ocrCancel()
			}
			job.ocr.Status = "cancelled"
			job.ocr.Error = "OCR остановлен при удалении в корзину"
			// Preserve a terminal state when this record is later restored.
			_, _ = d.core.MergeOCRAt(value.Library, id, job.ocr.InputSHA256, job.ocr)
			delete(d.recordJobs, key)
		}
	}
	d.selection.Unlock()
	return d.core.TrashScreenshots(value)
}

func (d *Desktop) ListTrash() ([]core.TrashEntry, error) { return d.core.ListTrash() }

func (d *Desktop) RestoreTrash(library string, tokens []string) (core.BatchResult, error) {
	d.operation.Lock()
	defer d.operation.Unlock()
	return d.core.RestoreTrash(library, tokens)
}

func (d *Desktop) CopyShelfFiles(value core.ShelfSelection, metadata bool) error {
	d.operation.Lock()
	defer d.operation.Unlock()
	paths, err := d.core.ShelfFilePaths(value, metadata)
	if err != nil {
		return err
	}
	return platform.CopyFiles(paths)
}

func (d *Desktop) CopyShelfText(value core.ShelfSelection) error {
	d.operation.Lock()
	defer d.operation.Unlock()
	if _, err := d.core.ShelfFilePaths(value, false); err != nil {
		return err
	}
	parts := []string{}
	for _, id := range value.IDs {
		record, err := d.core.GetRecord(id)
		if err != nil {
			return err
		}
		text := record.Note
		if record.OCR.InputSHA256 == record.ImageSHA256 {
			if ocr := core.EffectiveOCR(record.OCR); ocr != "" {
				text += "\n" + ocr
			}
		}
		parts = append(parts, record.ImageFile+"\n"+strings.TrimSpace(text))
	}
	return d.CopyOCRText(strings.Join(parts, "\n\n────────\n\n"))
}

func (d *Desktop) ExportShelfZIP(value core.ShelfSelection) (bool, error) {
	d.dialog.Lock()
	defer d.dialog.Unlock()
	ctx, err := d.nativeContext()
	if err != nil {
		return false, err
	}
	if _, err := d.core.ShelfFilePaths(value, false); err != nil {
		return false, err
	}
	path, err := wruntime.SaveFileDialog(ctx, wruntime.SaveDialogOptions{Title: "Архив выбранных снимков и метаданных", DefaultFilename: "ScreenShooter " + time.Now().Format("2006-01-02 15-04-05") + ".zip", Filters: []wruntime.FileFilter{{DisplayName: "ZIP-архив", Pattern: "*.zip"}}})
	if err != nil || path == "" {
		return false, err
	}
	if !strings.HasSuffix(strings.ToLower(path), ".zip") {
		path += ".zip"
	}
	d.operation.Lock()
	defer d.operation.Unlock()
	if err := d.core.ExportShelfZIP(value, path); err != nil {
		return false, err
	}
	return true, nil
}

func (d *Desktop) OpenShelfFolder() error {
	return platform.OpenFolder(d.core.CurrentSettings().LibraryDirectory)
}
