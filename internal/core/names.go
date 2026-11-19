package core

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const screenshotNamePrefix = "ScreenShorts Pro "
const screenshotNameTime = "2006-01-02 15-04-05"
const maxNameCollisions = 1000

// ScreenshotName uses the computer's local date and time. Filename colons are
// deliberately avoided so the same PNG/JSON pair can be copied to Windows.
func ScreenshotName(at time.Time) string { return screenshotNamePrefix + at.Format(screenshotNameTime) }

func availableScreenshotID(root *os.Root, at time.Time) (string, error) {
	base := ScreenshotName(at)
	for number := 1; number <= maxNameCollisions; number++ {
		id := base
		if number > 1 { id += fmt.Sprintf(" (%d)", number) }
		available := true
		for _, suffix := range []string{".png", ".png.json"} {
			if _, err := root.Lstat(id + suffix); err == nil { available = false; break
			} else if !errors.Is(err, os.ErrNotExist) { return "", fmt.Errorf("не удалось проверить имя снимка: %w", err) }
		}
		if available { return id, nil }
	}
	return "", errors.New("слишком много снимков с одинаковой датой и временем; повтори сохранение")
}

func validScreenshotName(id string) bool {
	value, ok := strings.CutPrefix(id, screenshotNamePrefix)
	// Accept names from the earlier source snapshot as well as legacy hex IDs.
	if !ok { value,ok=strings.CutPrefix(id,"Скринполка ") }
	if !ok || len(value) < len(screenshotNameTime) || len(value) > len(screenshotNameTime)+len(" (1000)") { return false }
	stamp, suffix := value[:len(screenshotNameTime)], value[len(screenshotNameTime):]
	parsed, err := time.Parse(screenshotNameTime, stamp)
	if err != nil || parsed.Format(screenshotNameTime) != stamp { return false }
	if suffix == "" { return true }
	if !strings.HasPrefix(suffix, " (") || !strings.HasSuffix(suffix, ")") { return false }
	digits := suffix[2:len(suffix)-1]
	number, err := strconv.Atoi(digits)
	return err == nil && number >= 2 && number <= maxNameCollisions && strconv.Itoa(number) == digits
}
