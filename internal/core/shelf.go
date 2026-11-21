package core

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxShelfBatch = 1000
const maxShelfExportBytes int64 = 1 << 30
const trashDirectory = ".screenshorts-trash"

type ShelfPreferences struct {
	View       string `json:"view"`
	Sort       string `json:"sort"`
	Descending bool   `json:"descending"`
	GroupDays  bool   `json:"group_days"`
	Compact    bool   `json:"compact"`
}

func (a *App) SaveShelfPreferences(value ShelfPreferences) (ShelfPreferences, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if value.View != "grid" && value.View != "table" {
		return value, errors.New("Неизвестный вид библиотеки")
	}
	switch value.Sort {
	case "date", "name", "content", "size", "resolution", "ocr":
	default:
		return value, errors.New("Неизвестная сортировка")
	}
	settings := a.settings
	settings.Shelf = value
	root, err := os.OpenRoot(a.dataRoot)
	if err != nil {
		return value, err
	}
	defer root.Close()
	if err := saveSettingsFile(root, settings); err != nil {
		return value, err
	}
	a.settings = settings
	return value, nil
}

// A selection belongs to a library, so a dialog or refresh cannot apply old IDs
// to a newly selected folder. No paths from sidecars are used as destinations.
type ShelfSelection struct {
	Library string   `json:"library"`
	IDs     []string `json:"ids"`
}

type BatchResult struct {
	Done      []string `json:"done"`
	Errors    []string `json:"errors"`
	Cancelled bool     `json:"cancelled"`
}

func (a *App) checkSelection(value ShelfSelection) error {
	if filepath.Clean(value.Library) != a.settings.LibraryDirectory {
		return errors.New("Библиотека сменилась. Выдели снимки заново")
	}
	if len(value.IDs) == 0 || len(value.IDs) > maxShelfBatch {
		return fmt.Errorf("Выбери от 1 до %d снимков за одно действие", maxShelfBatch)
	}
	seen := map[string]bool{}
	for _, id := range value.IDs {
		if !validID(id) || seen[id] {
			return errors.New("Некорректное или повторное выделение")
		}
		seen[id] = true
	}
	return nil
}

func readPair(root *os.Root, id string) (Screenshot, []byte, []byte, error) {
	record, err := loadScreenshot(root, id)
	if err != nil {
		return record, nil, nil, err
	}
	image, err := readRegular(root, id+".png", MaxPNGBytes)
	if err != nil {
		return record, nil, nil, err
	}
	hash := sha256.Sum256(image)
	if hex.EncodeToString(hash[:]) != record.ImageSHA256 {
		return record, nil, nil, errors.New("PNG изменён; SHA-256 не совпадает")
	}
	metadata, err := readRegular(root, id+".png.json", maxMetadataBytes)
	if err != nil {
		return record, nil, nil, err
	}
	digest := sha256.Sum256(metadata)
	if hex.EncodeToString(digest[:]) != record.MetadataDigest {
		return record, nil, nil, ErrRecordChanged
	}
	return record, image, metadata, nil
}

// Publish a complete file without overwriting anything, on both supported OSes.
func publishShelfFile(root *os.Root, name string, content []byte) error {
	temp, err := writeTemporary(root, content)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	return root.Link(temp, name)
}

func openTrash(library *os.Root) (*os.Root, error) {
	if err := library.Mkdir(trashDirectory, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := library.Lstat(trashDirectory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("Каталог корзины должен быть обычной папкой")
	}
	return library.OpenRoot(trashDirectory)
}

type TrashEntry struct {
	Token     string     `json:"token"`
	ID        string     `json:"id"`
	DeletedAt string     `json:"deleted_at"`
	Record    Screenshot `json:"record"`
}

func validTrashToken(token string) bool {
	if len(token) != 32 {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil && token == strings.ToLower(token)
}

func (a *App) TrashScreenshots(value ShelfSelection) (BatchResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := BatchResult{Done: []string{}, Errors: []string{}}
	if err := a.checkSelection(value); err != nil {
		return result, err
	}
	library, err := os.OpenRoot(a.settings.LibraryDirectory)
	if err != nil {
		return result, err
	}
	defer library.Close()
	trash, err := openTrash(library)
	if err != nil {
		return result, err
	}
	defer trash.Close()
	for _, id := range value.IDs {
		err := func() error {
			_, image, metadata, err := readPair(library, id)
			if err != nil {
				return err
			}
			token, err := randomID()
			if err != nil {
				return err
			}
			if err := trash.Mkdir(token, 0700); err != nil {
				return err
			}
			folder, err := trash.OpenRoot(token)
			if err != nil {
				return err
			}
			defer folder.Close()
			// Keep a complete recoverable copy before removing either source file.
			if err := publishShelfFile(folder, id+".png", image); err != nil {
				return err
			}
			if err := publishShelfFile(folder, id+".png.json", metadata); err != nil {
				return err
			}
			receipt, err := json.Marshal(TrashEntry{Token: token, ID: id, DeletedAt: time.Now().UTC().Format(time.RFC3339Nano)})
			if err != nil {
				return err
			}
			if err := publishShelfFile(folder, "receipt.json", receipt); err != nil {
				return err
			}
			current, _, _, err := readPair(library, id)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(metadata)
			if current.MetadataDigest != hex.EncodeToString(sum[:]) {
				return ErrRecordChanged
			}
			if err := library.Remove(id + ".png.json"); err != nil {
				return err
			}
			result.Done = append(result.Done, id)
			if err := library.Remove(id + ".png"); err != nil {
				return fmt.Errorf("Запись в корзине; исходный PNG остался: %w", err)
			}
			return nil
		}()
		if err != nil {
			result.Errors = append(result.Errors, id+": "+err.Error())
		}
	}
	return result, nil
}

func loadTrashEntry(trash *os.Root, token string) (TrashEntry, *os.Root, error) {
	var entry TrashEntry
	if !validTrashToken(token) {
		return entry, nil, errors.New("Некорректная запись корзины")
	}
	info, err := trash.Lstat(token)
	if err != nil {
		return entry, nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return entry, nil, errors.New("Небезопасная папка корзины")
	}
	folder, err := trash.OpenRoot(token)
	if err != nil {
		return entry, nil, err
	}
	data, err := readRegular(folder, "receipt.json", 8192)
	if err == nil {
		err = json.Unmarshal(data, &entry)
	}
	if err == nil && (entry.Token != token || !validID(entry.ID)) {
		err = errors.New("Повреждена запись корзины")
	}
	if err == nil {
		entry.Record, err = loadScreenshot(folder, entry.ID)
	}
	if err != nil {
		folder.Close()
		return entry, nil, err
	}
	return entry, folder, nil
}

func (a *App) ListTrash() ([]TrashEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	library, err := os.OpenRoot(a.settings.LibraryDirectory)
	if err != nil {
		return nil, err
	}
	defer library.Close()
	trash, err := openTrash(library)
	if err != nil {
		return nil, err
	}
	defer trash.Close()
	dir, err := trash.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return nil, err
	}
	result := []TrashEntry{}
	for _, item := range entries {
		entry, folder, err := loadTrashEntry(trash, item.Name())
		if err != nil {
			continue
		} // Interrupted staging files stay on disk, never auto-delete.
		folder.Close()
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].DeletedAt > result[j].DeletedAt })
	return result, nil
}

func (a *App) RestoreTrash(libraryPath string, tokens []string) (BatchResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := BatchResult{Done: []string{}, Errors: []string{}}
	if filepath.Clean(libraryPath) != a.settings.LibraryDirectory || len(tokens) == 0 || len(tokens) > maxShelfBatch {
		return result, errors.New("Библиотека сменилась или превышен лимит 1000 записей")
	}
	library, err := os.OpenRoot(a.settings.LibraryDirectory)
	if err != nil {
		return result, err
	}
	defer library.Close()
	trash, err := openTrash(library)
	if err != nil {
		return result, err
	}
	defer trash.Close()
	for _, token := range tokens {
		err := func() error {
			entry, folder, err := loadTrashEntry(trash, token)
			if err != nil {
				return err
			}
			defer folder.Close()
			_, image, metadata, err := readPair(folder, entry.ID)
			if err != nil {
				return err
			}
			// An interrupted move may leave an identical source file. Reuse it,
			// but never replace another version or unfamiliar file.
			missing := []string{}
			contents := map[string][]byte{}
			for index, data := range [][]byte{image, metadata} {
				name := entry.ID + ".png"
				if index == 1 {
					name += ".json"
				}
				existing, readErr := readRegular(library, name, MaxPNGBytes)
				if readErr == nil {
					if !bytes.Equal(existing, data) {
						return errors.New("Имя уже занято другой версией; восстановление остановлено")
					}
					continue
				}
				if !errors.Is(readErr, os.ErrNotExist) {
					return readErr
				}
				missing = append(missing, name)
				contents[name] = data
			}
			published := []string{}
			for _, name := range missing {
				if err := publishShelfFile(library, name, contents[name]); err != nil {
					for _, added := range published {
						err = errors.Join(err, library.Remove(added))
					}
					return err
				}
				published = append(published, name)
			}
			result.Done = append(result.Done, token)
			// Remove only these three known files; unknown contents are preserved.
			for _, name := range []string{entry.ID + ".png", entry.ID + ".png.json", "receipt.json"} {
				if err := folder.Remove(name); err != nil {
					return err
				}
			}
			folder.Close()
			return trash.Remove(token)
		}()
		if err != nil {
			result.Errors = append(result.Errors, token+": "+err.Error())
		}
	}
	return result, nil
}

func (a *App) ShelfFilePaths(value ShelfSelection, metadata bool) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.checkSelection(value); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(a.settings.LibraryDirectory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	paths := []string{}
	for _, id := range value.IDs {
		if _, err := loadScreenshot(root, id); err != nil {
			return nil, err
		}
		paths = append(paths, filepath.Join(a.settings.LibraryDirectory, id+".png"))
		if metadata {
			paths = append(paths, filepath.Join(a.settings.LibraryDirectory, id+".png.json"))
		}
	}
	return paths, nil
}

// ZIP is assembled one pair at a time. A failure leaves no partial destination.
// Original JSON bytes retain provenance and unfamiliar extension fields.
func (a *App) ExportShelfZIP(value ShelfSelection, destination string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.checkSelection(value); err != nil {
		return err
	}
	root, err := os.OpenRoot(a.settings.LibraryDirectory)
	if err != nil {
		return err
	}
	defer root.Close()
	if info, err := os.Lstat(destination); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("Архив должен быть обычным файлом")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".screenshorts-zip-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	archive := zip.NewWriter(file)
	var total int64
	for _, id := range value.IDs {
		record, image, metadata, err := readPair(root, id)
		if err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		total += int64(len(image) + len(metadata))
		if total > maxShelfExportBytes {
			return errors.New("Архив превышает 1 GiB; выбери меньше снимков")
		}
		for index, data := range [][]byte{image, metadata} {
			name := record.ImageFile
			method := uint16(zip.Store)
			if index == 1 {
				name += ".json"
				method = zip.Deflate
			}
			header := &zip.FileHeader{Name: name, Method: method}
			header.SetMode(0600)
			header.Modified, _ = time.Parse(time.RFC3339Nano, record.CreatedAt)
			out, err := archive.CreateHeader(header)
			if err != nil {
				return err
			}
			if _, err := out.Write(data); err != nil {
				return err
			}
		}
	}
	if err := archive.Close(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), destination)
}
