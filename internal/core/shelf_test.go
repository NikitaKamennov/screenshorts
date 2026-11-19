package core

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func shelfFixture(t *testing.T) (*App, Screenshot) {
	t.Helper()
	app, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 120, 80))
	img.Set(4, 4, color.NRGBA{R: 160, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	record, err := app.ImportPNGData(data.Bytes(), "ручной комментарий")
	if err != nil {
		t.Fatal(err)
	}
	ocr := OCRResult{InputSHA256: record.ImageSHA256, Status: "ready", RawText: "raw source", HasCorrection: true, CorrectedText: "исправленный текст"}
	record, err = app.MergeOCR(record.ID, record.ImageSHA256, ocr)
	if err != nil {
		t.Fatal(err)
	}
	return app, record
}

func TestShelfTrashRestoreAndZIP(t *testing.T) {
	app, record := shelfFixture(t)
	library := app.CurrentSettings().LibraryDirectory
	selection := ShelfSelection{Library: library, IDs: []string{record.ID}}
	original, err := os.ReadFile(filepath.Join(library, record.ImageFile+".json"))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := app.ShelfFilePaths(selection, true)
	if err != nil || len(paths) != 2 {
		t.Fatalf("paths: %v %v", paths, err)
	}
	snapshot, err := app.Snapshot()
	if err != nil || snapshot.Screenshots[0].ImageBytes <= 0 {
		t.Fatalf("file size: %+v %v", snapshot, err)
	}
	destination := filepath.Join(t.TempDir(), "screens.zip")
	if err := app.ExportShelfZIP(selection, destination); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 2 {
		t.Fatal("ZIP must contain PNG and JSON")
	}
	reader, err := archive.File[1].Open()
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(metadata, original) {
		t.Fatal("ZIP changed original metadata")
	}
	result, err := app.TrashScreenshots(selection)
	if err != nil || len(result.Done) != 1 || len(result.Errors) != 0 {
		t.Fatalf("trash: %+v %v", result, err)
	}
	snapshot, err = app.Snapshot()
	if err != nil || len(snapshot.Screenshots) != 0 || len(snapshot.Warnings) != 0 {
		t.Fatalf("trash leaked into shelf: %+v %v", snapshot, err)
	}
	entries, err := app.ListTrash()
	if err != nil || len(entries) != 1 {
		t.Fatalf("trash list: %+v %v", entries, err)
	}
	result, err = app.RestoreTrash(library, []string{entries[0].Token})
	if err != nil || len(result.Done) != 1 || len(result.Errors) != 0 {
		t.Fatalf("restore: %+v %v", result, err)
	}
	restored, err := app.GetRecord(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Note != record.Note || restored.OCR != record.OCR || restored.MetadataDigest != record.MetadataDigest {
		t.Fatal("restore lost provenance")
	}
	entries, err = app.ListTrash()
	if err != nil || len(entries) != 0 {
		t.Fatal("restored entry remains in trash")
	}
}

func TestShelfRejectsStaleLibraryChangedPNGAndDuplicates(t *testing.T) {
	app, record := shelfFixture(t)
	library := app.CurrentSettings().LibraryDirectory
	for _, selection := range []ShelfSelection{{Library: library + "-other", IDs: []string{record.ID}}, {Library: library, IDs: []string{record.ID, record.ID}}, {Library: library, IDs: []string{"../outside"}}} {
		if _, err := app.TrashScreenshots(selection); err == nil {
			t.Fatal("unsafe selection accepted")
		}
	}
	if err := os.WriteFile(filepath.Join(library, record.ImageFile), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := app.TrashScreenshots(ShelfSelection{Library: library, IDs: []string{record.ID}})
	if err != nil || len(result.Done) != 0 || len(result.Errors) != 1 {
		t.Fatalf("changed PNG: %+v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(library, record.ImageFile+".json")); err != nil {
		t.Fatal("invalid PNG caused metadata deletion")
	}
	destination := filepath.Join(t.TempDir(), "bad.zip")
	if err := app.ExportShelfZIP(ShelfSelection{Library: library, IDs: []string{record.ID}}, destination); err == nil {
		t.Fatal("changed PNG exported")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("failed ZIP published")
	}
}

func TestShelfRestoreConflictDoesNotPublishHalfPair(t *testing.T) {
	app, record := shelfFixture(t)
	library := app.CurrentSettings().LibraryDirectory
	_, err := app.TrashScreenshots(ShelfSelection{Library: library, IDs: []string{record.ID}})
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := app.ListTrash()
	path := filepath.Join(library, record.ImageFile+".json")
	if err := os.WriteFile(path, []byte("another version"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := app.RestoreTrash(library, []string{entries[0].Token})
	if err != nil || len(result.Done) != 0 || len(result.Errors) != 1 {
		t.Fatalf("conflict accepted: %+v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(library, record.ImageFile)); !os.IsNotExist(err) {
		t.Fatal("half pair published on conflict")
	}
	current, _ := os.ReadFile(path)
	if string(current) != "another version" {
		t.Fatal("existing metadata overwritten")
	}
	entries, _ = app.ListTrash()
	if len(entries) != 1 {
		t.Fatal("conflicting trash entry lost")
	}
}

func TestShelfPreferencesSurviveOtherSettings(t *testing.T) {
	app, _ := shelfFixture(t)
	prefs := ShelfPreferences{View: "table", Sort: "size", GroupDays: true, Compact: true}
	if _, err := app.SaveShelfPreferences(prefs); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SaveAppearance(Appearance{Theme: "dark"}); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(app.dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.CurrentSettings().Shelf != prefs {
		t.Fatal("shelf settings not preserved")
	}
}
