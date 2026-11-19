package analysis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"skrinpolka/internal/core"
	"skrinpolka/internal/sandbox"
)

type BackgroundConfig struct {
	LinuxRuntimeRoot string   `json:"linux_runtime_root,omitempty"`
	Enabled          bool     `json:"enabled"`
	Executable       string   `json:"executable"`
	Script           string   `json:"script"`
	ModelPath        string   `json:"model_path"`
	ModelSHA256      string   `json:"model_sha256"`
	ModelID          string   `json:"model_id"`
	ReadRoots        []string `json:"read_roots"`
	MemoryBytes      uint64   `json:"memory_bytes"`
	Threads          int      `json:"threads"`
	TimeoutSeconds   int      `json:"timeout_seconds"`
}

type BackgroundService struct {
	config   BackgroundConfig
	workRoot string
	queue    *Queue
	problem  string
}

func NewBackground(configFile, workRoot string, queue *Queue) *BackgroundService {
	s := &BackgroundService{workRoot: workRoot, queue: queue}
	file, err := os.Open(configFile)
	if err != nil {
		s.problem = "Нет профиля удаления фона; выполни подготовку компонентов"
		return s
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(content) > 65536 {
		s.problem = "Некорректный размер профиля удаления фона"
		return s
	}
	var config RuntimeConfig
	if err := json.Unmarshal(content, &config); err != nil || config.Schema != "skrinpolka-runtime-v1" {
		s.problem = "Некорректный профиль удаления фона"
		return s
	}
	s.config = config.Background
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
		return filepath.Clean(absolute)
	}
	s.config.Executable = resolve(s.config.Executable)
	s.config.Script = resolve(s.config.Script)
	s.config.ModelPath = resolve(s.config.ModelPath)
	for index, root := range s.config.ReadRoots {
		s.config.ReadRoots[index] = resolve(root)
	}
	if s.config.LinuxRuntimeRoot != "" {
		s.config.LinuxRuntimeRoot = resolve(s.config.LinuxRuntimeRoot)
	}
	return s
}

func (s *BackgroundService) Readiness() Readiness {
	ready := Readiness{ModelID: s.config.ModelID}
	if s.problem != "" {
		ready.Reason = s.problem
		return ready
	}
	if !s.config.Enabled {
		ready.Reason = "Удаление фона не подключено; смотри подготовку компонентов в справке"
		return ready
	}
	for _, path := range []string{s.config.Executable, s.config.Script, s.config.ModelPath} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			ready.Reason = "Не установлены компоненты удаления фона; выполни подготовку и перезапусти приложение"
			return ready
		}
	}
	if s.config.ModelID != "u2netp" || len(s.config.ModelSHA256) != 64 {
		ready.Reason = "Нужен профиль U²-NetP с SHA-256 модели"
		return ready
	}
	if _, err := hex.DecodeString(s.config.ModelSHA256); err != nil {
		ready.Reason = "Некорректный SHA-256 модели фона"
		return ready
	}
	if len(s.config.ReadRoots) == 0 || s.config.MemoryBytes < 256<<20 || s.config.MemoryBytes > 2<<30 || s.config.Threads < 1 || s.config.Threads > 4 || s.config.TimeoutSeconds < 1 || s.config.TimeoutSeconds > 300 {
		ready.Reason = "Некорректные лимиты удаления фона"
		return ready
	}
	ready.Available = true
	ready.Reason = "Локальное удаление фона: U²-NetP, CPU"
	return ready
}

func (s *BackgroundService) Remove(ctx context.Context, input []byte, onStart func()) ([]byte, error) {
	if ready := s.Readiness(); !ready.Available {
		return nil, fmt.Errorf("%s", ready.Reason)
	}
	release, err := s.queue.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if onStart != nil {
		onStart()
	}
	model, err := os.Open(s.config.ModelPath)
	if err != nil {
		return nil, err
	}
	info, err := model.Stat()
	if err != nil || info.Size() > 8<<20 {
		model.Close()
		return nil, fmt.Errorf("Модель U²-NetP превышает допустимый размер")
	}
	hash := sha256.New()
	_, hashErr := io.Copy(hash, io.LimitReader(model, (8<<20)+1))
	closeErr := model.Close()
	if hashErr != nil || closeErr != nil || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), s.config.ModelSHA256) {
		return nil, fmt.Errorf("SHA-256 модели удаления фона не совпадает с профилем")
	}
	if err := os.MkdirAll(s.workRoot, 0700); err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp(s.workRoot, "background-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	if err := sandbox.PrepareDirectory(directory); err != nil {
		return nil, fmt.Errorf("Не удалось защитить каталог удаления фона: %w", err)
	}
	inputPath, outputPath := filepath.Join(directory, "input.png"), filepath.Join(directory, "output.png")
	if err := os.WriteFile(inputPath, input, 0600); err != nil {
		return nil, err
	}
	arguments := []string{"-I", "-B", s.config.Script, "--input", inputPath, "--output", outputPath, "--model", s.config.ModelPath, "--sha256", s.config.ModelSHA256, "--threads", strconv.Itoa(s.config.Threads)}
	if err := sandbox.Run(ctx, sandbox.Spec{RuntimeRoot: s.config.LinuxRuntimeRoot, Executable: s.config.Executable, Arguments: arguments, ReadRoots: s.config.ReadRoots, Directory: directory, MemoryBytes: s.config.MemoryBytes, Threads: s.config.Threads, TimeoutSeconds: s.config.TimeoutSeconds}); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	outputInfo, err := root.Lstat("output.png")
	if err != nil || !outputInfo.Mode().IsRegular() || outputInfo.Size() > core.MaxPNGBytes {
		return nil, fmt.Errorf("Worker не вернул допустимый PNG до 20 MiB")
	}
	output, err := root.Open("output.png")
	if err != nil {
		return nil, err
	}
	defer output.Close()
	data, err := io.ReadAll(io.LimitReader(output, core.MaxPNGBytes+1))
	if err != nil {
		return nil, err
	}
	return core.PreparePNGData(data)
}
