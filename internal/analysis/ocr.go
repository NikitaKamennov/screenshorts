package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"skrinpolka/internal/sandbox"
)

type OCRConfig struct {
	LinuxRuntimeRoot  string   `json:"linux_runtime_root,omitempty"`
	Enabled           bool     `json:"enabled"`
	Executable        string   `json:"executable"`
	TessdataDirectory string   `json:"tessdata_directory"`
	Languages         []string `json:"languages"`
	ReadRoots         []string `json:"read_roots"`
	MemoryBytes       uint64   `json:"memory_bytes"`
	Threads           int      `json:"threads"`
	TimeoutSeconds    int      `json:"timeout_seconds"`
	ModelID           string   `json:"model_id"`
	ModelRevision     string   `json:"model_revision"`
}

type RuntimeConfig struct {
	Schema     string           `json:"schema"`
	OCR        OCRConfig        `json:"ocr"`
	Background BackgroundConfig `json:"background_removal"`
}
type Readiness struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
	ModelID   string `json:"model_id"`
}

// OCRService owns a single protected worker slot. No text/image is written to
// persistent logs; private per-task files are removed after process termination.
type OCRService struct {
	config   OCRConfig
	workRoot string
	queue    *Queue
	problem  string
}

func NewOCR(configFile, workRoot string, queue *Queue) *OCRService {
	s := &OCRService{workRoot: workRoot, queue: queue}
	content, err := os.ReadFile(configFile)
	if err != nil {
		s.problem = "Нет конфигурации локального OCR: " + configFile
		return s
	}
	if len(content) > 65536 {
		s.problem = "Конфигурация OCR превышает допустимый размер"
		return s
	}
	var config RuntimeConfig
	if err := json.Unmarshal(content, &config); err != nil || config.Schema != "skrinpolka-runtime-v1" {
		s.problem = "Неподдерживаемая конфигурация OCR"
		return s
	}
	s.config = config.OCR
	resolve := func(path string) string {
		if path == "" {
			return ""
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(configFile), path)
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return ""
		}
		return absolute
	}
	s.config.Executable = resolve(s.config.Executable)
	s.config.TessdataDirectory = resolve(s.config.TessdataDirectory)
	for index, root := range s.config.ReadRoots {
		s.config.ReadRoots[index] = resolve(root)
	}
	if s.config.LinuxRuntimeRoot != "" {
		s.config.LinuxRuntimeRoot = resolve(s.config.LinuxRuntimeRoot)
	}
	return s
}

func (s *OCRService) Readiness() Readiness {
	ready := Readiness{ModelID: s.config.ModelID}
	if s.problem != "" {
		ready.Reason = s.problem
		return ready
	}
	if !s.config.Enabled {
		ready.Reason = "Локальный OCR отключён в конфигурации"
		return ready
	}
	if info, err := os.Stat(s.config.Executable); err != nil || !info.Mode().IsRegular() {
		ready.Reason = "Не установлен локальный Tesseract"
		return ready
	}
	if len(s.config.Languages) == 0 {
		ready.Reason = "Не настроены языки OCR"
		return ready
	}
	for _, language := range s.config.Languages {
		if language != "rus" && language != "eng" {
			ready.Reason = "Поддержаны русская и английская модели OCR"
			return ready
		}
		if info, err := os.Stat(filepath.Join(s.config.TessdataDirectory, language+".traineddata")); err != nil || !info.Mode().IsRegular() {
			ready.Reason = "Нет языковой модели OCR: " + language
			return ready
		}
	}
	if len(s.config.ReadRoots) == 0 {
		ready.Reason = "Не настроены read-only каталоги OCR"
		return ready
	}
	if s.config.Threads < 1 || s.config.Threads > 4 || s.config.MemoryBytes < 128<<20 || s.config.MemoryBytes > 4<<30 || s.config.TimeoutSeconds < 1 || s.config.TimeoutSeconds > 300 {
		ready.Reason = "Некорректные лимиты OCR"
		return ready
	}
	ready.Available = true
	ready.Reason = "Локальный Tesseract: " + strings.Join(s.config.Languages, "+")
	return ready
}

func (s *OCRService) ModelRevision() string { return s.config.ModelRevision }

func (s *OCRService) Recognize(ctx context.Context, inputData func() ([]byte, error), onStart func()) (string, error) {
	if ready := s.Readiness(); !ready.Available {
		return "", fmt.Errorf("%s", ready.Reason)
	}
	release, err := s.queue.Acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if onStart != nil {
		onStart()
	}
	png, err := inputData()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.workRoot, 0700); err != nil {
		return "", err
	}
	directory, err := os.MkdirTemp(s.workRoot, "ocr-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(directory)
	if err := sandbox.PrepareDirectory(directory); err != nil {
		return "", fmt.Errorf("Не удалось защитить каталог OCR: %w", err)
	}
	input, output := filepath.Join(directory, "input.png"), filepath.Join(directory, "result")
	if err := os.WriteFile(input, png, 0600); err != nil {
		return "", err
	}
	arguments := []string{input, output, "--tessdata-dir", s.config.TessdataDirectory, "-l", strings.Join(s.config.Languages, "+"), "--oem", "1", "--psm", "11"}
	if err := sandbox.Run(ctx, sandbox.Spec{RuntimeRoot: s.config.LinuxRuntimeRoot, Executable: s.config.Executable, Arguments: arguments, ReadRoots: s.config.ReadRoots, Directory: directory, MemoryBytes: s.config.MemoryBytes, Threads: s.config.Threads, TimeoutSeconds: s.config.TimeoutSeconds}); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if info, err := root.Lstat("result.txt"); err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("OCR не создал безопасный текстовый результат")
	}
	file, err := root.Open("result.txt")
	if err != nil {
		return "", fmt.Errorf("OCR не создал текстовый результат")
	}
	defer file.Close()
	const limit = 512 << 10
	text, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(text) > limit || !utf8.Valid(text) {
		return "", fmt.Errorf("OCR вернул повреждённый или слишком большой текст")
	}
	return strings.TrimSpace(strings.TrimPrefix(string(text), "\ufeff")), nil
}
