package core

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Appearance struct {
	Theme string `json:"theme"`
	AccentColor string `json:"accent_color"`
}

func normalizeAppearance(value Appearance) (Appearance, error) {
	if value.Theme == "" { value.Theme = "light" }
	if value.Theme != "light" && value.Theme != "dark" { return Appearance{}, fmt.Errorf("Выбери светлую или тёмную тему") }
	if value.AccentColor == "" { value.AccentColor = "#2d6a4e" }
	if len(value.AccentColor) != 7 || value.AccentColor[0] != '#' { return Appearance{}, fmt.Errorf("Цвет должен иметь вид #RRGGBB") }
	if _, err := strconv.ParseUint(value.AccentColor[1:], 16, 24); err != nil { return Appearance{}, fmt.Errorf("Некорректный цвет интерфейса") }
	value.AccentColor = strings.ToLower(value.AccentColor)
	return value, nil
}

// SaveAppearance changes only theme and accent, keeping native and layout prefs.
func (a *App) SaveAppearance(value Appearance) (Appearance, error) {
	a.mu.Lock(); defer a.mu.Unlock()
	value, err := normalizeAppearance(value)
	if err != nil { return Appearance{}, err }
	settings := a.settings
	settings.Theme, settings.AccentColor = value.Theme, value.AccentColor
	root, err := os.OpenRoot(a.dataRoot)
	if err != nil { return Appearance{}, err }; defer root.Close()
	if err := saveSettingsFile(root, settings); err != nil { return Appearance{}, err }
	a.settings = settings
	return value, nil
}
