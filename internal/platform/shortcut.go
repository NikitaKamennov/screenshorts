package platform

import (
	"fmt"
	"strconv"
	"strings"
)

const DefaultShortcut = "Ctrl+Shift+PrintScreen"
const MaxCapturePixels = 32_000_000

type Shortcuts struct { Area, Screen, Silent, SilentArea string }

func NormalizeShortcuts(shortcuts Shortcuts) (Shortcuts, error) {
	area, err := ParseShortcut(shortcuts.Area)
	if err != nil { return Shortcuts{}, fmt.Errorf("Комбинация области: %w", err) }
	screen, err := ParseShortcut(shortcuts.Screen)
	if err != nil { return Shortcuts{}, fmt.Errorf("Комбинация всего экрана: %w", err) }
	silent, err := ParseShortcut(shortcuts.Silent)
	if err != nil { return Shortcuts{}, fmt.Errorf("Комбинация тихого снимка: %w", err) }
	silentArea, err := ParseShortcut(shortcuts.SilentArea)
	if err != nil { return Shortcuts{}, fmt.Errorf("Комбинация тихой области: %w", err) }
	result := Shortcuts{Area: area.String(), Screen: screen.String(), Silent: silent.String(), SilentArea: silentArea.String()}
	seen := map[string]bool{}
	for _, text := range []string{result.Area, result.Screen, result.Silent, result.SilentArea} {
		if text == "" { continue }
		if seen[text] { return Shortcuts{}, fmt.Errorf("Для четырёх действий захвата нужны разные комбинации") }
		seen[text] = true
	}
	return result, nil
}

func shortcutFeature(text, reason string) Feature {
	if text == "" { return Feature{Reason: "Комбинация отключена"} }
	return Feature{Available: true, Reason: reason}
}

type Shortcut struct {
	Shift, Ctrl, Alt, Super bool
	Key string
}

func ParseShortcut(text string) (Shortcut, error) {
	var result Shortcut
	if strings.TrimSpace(text) == "" { return result, nil }
	for _, raw := range strings.Split(text, "+") {
		part := strings.ToUpper(strings.TrimSpace(raw))
		switch part {
		case "CTRL", "CONTROL": result.Ctrl = true
		case "SHIFT": result.Shift = true
		case "ALT": result.Alt = true
		case "SUPER", "WIN", "META": result.Super = true
		default:
			if result.Key != "" { return Shortcut{}, fmt.Errorf("Укажи одну клавишу и модификаторы") }
			if part == "PRINT" || part == "PRINTSCREEN" { part = "PrintScreen" }
			valid := len(part) == 1 && (part[0] >= 'A' && part[0] <= 'Z' || part[0] >= '0' && part[0] <= '9')
			if strings.HasPrefix(part, "F") { number, err := strconv.Atoi(part[1:]); valid = valid || err == nil && number >= 1 && number <= 24 }
			if !valid && part != "PrintScreen" { return Shortcut{}, fmt.Errorf("Поддержаны A–Z, 0–9, F1–F24 и PrintScreen") }
			result.Key = part
		}
	}
	if result.Key == "" || !(result.Ctrl || result.Shift || result.Alt || result.Super) {
		return Shortcut{}, fmt.Errorf("Добавь модификатор Ctrl, Shift, Alt или Super")
	}
	return result, nil
}

func (s Shortcut) String() string {
	if s.Key == "" { return "" }
	parts := []string{}
	if s.Ctrl { parts = append(parts, "Ctrl") }
	if s.Shift { parts = append(parts, "Shift") }
	if s.Alt { parts = append(parts, "Alt") }
	if s.Super { parts = append(parts, "Super") }
	return strings.Join(append(parts, s.Key), "+")
}
