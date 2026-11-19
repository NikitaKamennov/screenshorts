// Build-time file preparation only. This program never loads or executes a
// browser, application, OCR engine, Python interpreter or model.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/andybalholm/brotli"
)

const maxPackBytes = 128 << 20
const maxNoticeBytes = 16 << 20

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative) && !strings.Contains(relative, ":")
}

func inputPath(root, path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil || !within(root, absolute) {
		return "", errors.New("input is outside the project")
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil || !within(root, real) {
		return "", errors.New("input resolves outside the project")
	}
	return real, nil
}

// Microsoft WebView2 157 uses 32-bit IDs/counts in a v5 resource pack. Its
// header is 16 bytes, entries and aliases are 8 bytes each. This layout was
// read from the pinned CAB; it is not the upstream Chromium uint16 layout.
func resource(pack []byte, wanted uint32) ([]byte, error) {
	if len(pack) < 32 || binary.LittleEndian.Uint32(pack[:4]) != 5 || pack[4] != 1 || !bytes.Equal(pack[5:8], []byte{0, 0, 0}) {
		return nil, errors.New("unsupported WebView2 resource pack header")
	}
	count := uint64(binary.LittleEndian.Uint32(pack[8:12]))
	aliases := uint64(binary.LittleEndian.Uint32(pack[12:16]))
	dataStart := 16 + (count+1)*8 + aliases*8
	if count == 0 || count > 20000 || aliases > 20000 || dataStart > uint64(len(pack)) {
		return nil, errors.New("resource table exceeds its bounds")
	}
	var selected []byte
	var previousID uint32
	for index := uint64(0); index < count; index++ {
		entry := 16 + index*8
		id := binary.LittleEndian.Uint32(pack[entry : entry+4])
		start := uint64(binary.LittleEndian.Uint32(pack[entry+4 : entry+8]))
		end := uint64(binary.LittleEndian.Uint32(pack[entry+12 : entry+16]))
		if id == 0 || (index > 0 && id <= previousID) || start < dataStart || end < start || end > uint64(len(pack)) {
			return nil, errors.New("invalid resource ID or offset")
		}
		if index == 0 && start != dataStart {
			return nil, errors.New("unexpected data table padding")
		}
		if id == wanted {
			selected = pack[start:end]
		}
		previousID = id
	}
	last := 16 + count*8
	if binary.LittleEndian.Uint32(pack[last:last+4]) != 0 || uint64(binary.LittleEndian.Uint32(pack[last+4:last+8])) != uint64(len(pack)) {
		return nil, errors.New("invalid resource table sentinel")
	}
	for index := uint64(0); index < aliases; index++ {
		entry := last + 8 + index*8
		if uint64(binary.LittleEndian.Uint32(pack[entry+4:entry+8])) >= count {
			return nil, errors.New("alias points outside the resource table")
		}
	}
	if selected == nil {
		return nil, errors.New("requested resource is not a main-table entry")
	}
	return selected, nil
}

func decode(payload []byte) ([]byte, error) {
	if len(payload) < 9 || payload[0] != 0x1e || payload[1] != 0x9b {
		return nil, errors.New("expected a Brotli-compressed resource")
	}
	var expected uint64
	for index := 0; index < 6; index++ {
		expected |= uint64(payload[2+index]) << (index*8)
	}
	if expected == 0 || expected > maxNoticeBytes {
		return nil, errors.New("notice exceeds the 16 MiB limit")
	}
	result, err := io.ReadAll(io.LimitReader(brotli.NewReader(bytes.NewReader(payload[8:])), int64(expected)+1))
	if err != nil {
		return nil, fmt.Errorf("decode resource: %w", err)
	}
	if uint64(len(result)) != expected {
		return nil, errors.New("decoded size does not match the resource")
	}
	return result, nil
}

func run() error {
	rootFlag := flag.String("project-root", "", "project boundary")
	packFlag := flag.String("pack", "", "pinned resources.pak")
	packHashFlag := flag.String("pack-sha256", "", "required resource pack SHA-256")
	idFlag := flag.Uint("resource", 0, "32-bit main-table resource ID")
	outputFlag := flag.String("output", "", "new output file inside the project")
	outputHashFlag := flag.String("output-sha256", "", "expected decoded SHA-256, once pinned")
	catalogFlag := flag.Bool("catalog", false, "list possible notice resources without executing them")
	flag.Parse()
	if flag.NArg() != 0 || *rootFlag == "" || *packFlag == "" || (!*catalogFlag && (*outputFlag == "" || *idFlag == 0)) || uint64(*idFlag) > 0xffffffff {
		return errors.New("project-root, pack, resource, output and pack-sha256 are required")
	}
	hashBytes, err := hex.DecodeString(*packHashFlag)
	if err != nil || len(hashBytes) != 32 {
		return errors.New("invalid pack-sha256")
	}
	root, err := filepath.Abs(*rootFlag)
	if err != nil { return err }
	root, err = filepath.EvalSymlinks(root)
	if err != nil { return err }
	packPath, err := inputPath(root, *packFlag)
	if err != nil { return err }
	input, err := os.Open(packPath)
	if err != nil { return err }
	pack, readErr := io.ReadAll(io.LimitReader(input, maxPackBytes+1))
	closeErr := input.Close()
	if readErr != nil || closeErr != nil { return errors.Join(readErr, closeErr) }
	if len(pack) > maxPackBytes { return errors.New("resource pack exceeds the 128 MiB limit") }
	packHash := sha256.Sum256(pack)
	if !bytes.Equal(packHash[:], hashBytes) { return errors.New("resource pack SHA-256 mismatch") }
	if *catalogFlag {
		if len(pack) < 32 { return errors.New("resource pack is too short") }
		if _, err := resource(pack, binary.LittleEndian.Uint32(pack[16:20])); err != nil { return err }
		type entry struct {
			ID uint32 `json:"id"`
			Bytes int `json:"bytes"`
			Copyrights int `json:"copyrights"`
			Licenses int `json:"licenses"`
			Title string `json:"title,omitempty"`
		}
		var entries []entry
		var total int
		title := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
		for index, count := uint32(0), binary.LittleEndian.Uint32(pack[8:12]); index < count; index++ {
			offset := 16 + uint64(index)*8
			start := binary.LittleEndian.Uint32(pack[offset+4:offset+8])
			end := binary.LittleEndian.Uint32(pack[offset+12:offset+16])
			payload := pack[start:end]
			if len(payload) < 9 || payload[0] != 0x1e || payload[1] != 0x9b { continue }
			data, err := decode(payload)
			if err != nil { return err }
			if !utf8.Valid(data) { continue }
			total += len(data)
			if total > maxPackBytes { return errors.New("catalog exceeds its total decoding limit") }
			lower := strings.ToLower(string(data))
			item := entry{ID:binary.LittleEndian.Uint32(pack[offset:offset+4]), Bytes:len(data), Copyrights:strings.Count(lower,"copyright"), Licenses:strings.Count(lower,"license")}
			if matches := title.FindSubmatch(data); len(matches)>1 { item.Title = string(matches[1]) }
			if item.Copyrights > 30 || item.Licenses > 100 || item.Title != "" { entries = append(entries,item) }
		}
		return json.NewEncoder(os.Stdout).Encode(entries)
	}
	payload, err := resource(pack, uint32(*idFlag))
	if err != nil { return err }
	notice, err := decode(payload)
	if err != nil { return err }
	if !utf8.Valid(notice) { return errors.New("notice is not UTF-8 text") }
	noticeHash := sha256.Sum256(notice)
	encodedHash := hex.EncodeToString(noticeHash[:])
	if *outputHashFlag != "" && encodedHash != *outputHashFlag { return errors.New("decoded notice SHA-256 mismatch") }
	output, err := filepath.Abs(*outputFlag)
	if err != nil || !within(root, output) { return errors.New("output is outside the project") }
	realParent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil || !within(root, realParent) { return errors.New("output parent resolves outside the project") }
	file, err := os.OpenFile(filepath.Join(realParent, filepath.Base(output)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil { return err }
	_, writeErr := file.Write(notice)
	syncErr := file.Sync()
	closeErr = file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil { return errors.Join(writeErr, syncErr, closeErr) }
	return json.NewEncoder(os.Stdout).Encode(struct {
		Schema string `json:"schema"`
		Resource uint32 `json:"resource"`
		Bytes int `json:"bytes"`
		SHA256 string `json:"sha256"`
	}{"screenshorts-resource-notice-v1", uint32(*idFlag), len(notice), encodedHash})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "notice extraction:", err)
		os.Exit(1)
	}
}
