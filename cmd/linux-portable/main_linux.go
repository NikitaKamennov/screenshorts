//go:build linux

// A static, FUSE-free launcher with a hash-checked appended tar.gz payload.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const footerSize = 48
const magic = "SCPLNX01"

func privateDirectory(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("нужен абсолютный путь к кэшу")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || stat.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("небезопасный каталог кэша: %s", path)
	}
	return os.Chmod(path, 0700)
}

func extract(file *os.File, offset, size int64, expected []byte, destination string) error {
	hash := sha256.New()
	if _, err := io.Copy(hash, io.NewSectionReader(file, offset, size)); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != hex.EncodeToString(expected) {
		return fmt.Errorf("SHA-256 комплекта не совпадает")
	}
	zipped, err := gzip.NewReader(io.NewSectionReader(file, offset, size))
	if err != nil {
		return err
	}
	defer zipped.Close()
	reader := tar.NewReader(zipped)
	var total int64
	for count := 0; ; count++ {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if count >= 100000 {
			return fmt.Errorf("слишком много файлов в комплекте")
		}
		name := filepath.Clean(header.Name)
		if !filepath.IsLocal(name) || len(name) > 4096 || strings.ContainsRune(name, '\\') ||
			(name != "ScreenShooterPro" && !strings.HasPrefix(name, "ScreenShooterPro/")) {
			return fmt.Errorf("недопустимый путь в комплекте")
		}
		target := filepath.Join(destination, name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			total += header.Size
			if header.Size < 0 || header.Size > 1<<30 || total > 4<<30 {
				return fmt.Errorf("комплект превышает предел распаковки")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			mode := os.FileMode(0644)
			if header.Mode&0111 != 0 {
				mode = 0755
			}
			output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(output, reader, header.Size)
			closeErr := output.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeLink:
			link := filepath.Clean(header.Linkname)
			if !filepath.IsLocal(link) || !strings.HasPrefix(link, "ScreenShooterPro/") {
				return fmt.Errorf("недопустимая ссылка в комплекте")
			}
			source := filepath.Join(destination, link)
			info, err := os.Lstat(source)
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("неверная цель ссылки комплекта")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := os.Link(source, target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("неподдерживаемый тип файла в комплекте")
		}
	}
	return nil
}

func run() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	file, err := os.Open(executable)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() < footerSize {
		return fmt.Errorf("исполняемый комплект повреждён")
	}
	footer := make([]byte, footerSize)
	if _, err := file.ReadAt(footer, info.Size()-footerSize); err != nil {
		return err
	}
	if string(footer[:8]) != magic {
		return fmt.Errorf("в launcher отсутствует payload")
	}
	size := int64(binary.LittleEndian.Uint64(footer[8:16]))
	if size <= 0 || size > 4<<30 || size > info.Size()-footerSize {
		return fmt.Errorf("некорректный размер payload")
	}
	id := hex.EncodeToString(footer[16:])
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	cache = filepath.Join(cache, "skrinpolka", "portable")
	if err := privateDirectory(cache); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(cache, ".extract.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	destination := filepath.Join(cache, id)
	if _, err := os.Lstat(destination); os.IsNotExist(err) {
		temporary, err := os.MkdirTemp(cache, ".unpack-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(temporary)
		if err := extract(file, info.Size()-footerSize-size, size, footer[16:], temporary); err != nil {
			return err
		}
		folder := filepath.Join(temporary, "ScreenShooterPro")
		if err := os.WriteFile(filepath.Join(folder, ".complete"), []byte(id), 0600); err != nil {
			return err
		}
		if err := os.Rename(folder, destination); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := privateDirectory(destination); err != nil {
		return err
	}
	marker, err := os.ReadFile(filepath.Join(destination, ".complete"))
	if err != nil || string(marker) != id {
		return fmt.Errorf("неполный кэш: %s", destination)
	}
	launcher := filepath.Join(destination, "ScreenShooterPro")
	entry, err := os.Lstat(launcher)
	if err != nil || !entry.Mode().IsRegular() {
		return fmt.Errorf("launcher комплекта отсутствует")
	}
	// The cache lock must not survive into the desktop application.
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		return err
	}
	lock.Close()
	return syscall.Exec(launcher, append([]string{launcher}, os.Args[1:]...), os.Environ())
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ScreenShooter Pro:", err)
		os.Exit(1)
	}
}
