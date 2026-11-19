package core

import (
	"fmt"
	"os"
)

type EditorLayout struct {
	Position string `json:"position"`
	ToolOrder []string `json:"tool_order"`
	PanelWidth int `json:"panel_width"`
	PanelHeight int `json:"panel_height"`
	Width int `json:"width"`
	Height int `json:"height"`
}

func normalizeEditorLayout(layout EditorLayout) (EditorLayout, error) {
	if layout.Position == "" { layout.Position = "right" }
	switch layout.Position {
	case "right", "bottom", "left", "top":
	default: return EditorLayout{}, fmt.Errorf("Неизвестное положение панели")
	}
	if len(layout.ToolOrder) > 32 { return EditorLayout{}, fmt.Errorf("Слишком много блоков панели") }
	defaults := []string{"comment", "drawing", "ocr", "photo"}
	known := map[string]bool{}
	for _, id := range defaults { known[id] = true }
	seen := map[string]bool{}
	order := make([]string, 0, len(defaults))
	for _, id := range layout.ToolOrder {
		if known[id] && !seen[id] { order = append(order, id); seen[id] = true }
	}
	for _, id := range defaults { if !seen[id] { order = append(order, id) } }
	layout.ToolOrder = order
	for _, size := range []struct{ value *int; fallback, minimum, maximum int }{
		{&layout.PanelWidth, 320, 240, 1600}, {&layout.PanelHeight, 280, 140, 1200},
		{&layout.Width, 1200, 680, 3840}, {&layout.Height, 900, 480, 2160},
	} {
		if *size.value == 0 { *size.value = size.fallback }
		*size.value = max(size.minimum, min(size.maximum, *size.value))
	}
	return layout, nil
}

func LayoutFromSettings(s Settings) EditorLayout {
	return EditorLayout{Position:s.EditorPanelPosition, ToolOrder:s.EditorToolOrder, PanelWidth:s.EditorPanelWidth, PanelHeight:s.EditorPanelHeight, Width:s.EditorWidth, Height:s.EditorHeight}
}

func applyEditorLayout(s *Settings, layout EditorLayout) {
	s.EditorPanelPosition, s.EditorToolOrder = layout.Position, layout.ToolOrder
	s.EditorPanelWidth, s.EditorPanelHeight = layout.PanelWidth, layout.PanelHeight
	s.EditorWidth, s.EditorHeight = layout.Width, layout.Height
}

// SaveEditorLayout changes only layout preferences; an old UI snapshot cannot
// overwrite a newer library, shortcut or OCR setting through this operation.
func (a *App) SaveEditorLayout(layout EditorLayout) (EditorLayout, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	layout, err := normalizeEditorLayout(layout)
	if err != nil { return EditorLayout{}, err }
	settings := a.settings
	applyEditorLayout(&settings, layout)
	root, err := os.OpenRoot(a.dataRoot)
	if err != nil { return EditorLayout{}, err }
	defer root.Close()
	if err := saveSettingsFile(root, settings); err != nil { return EditorLayout{}, err }
	a.settings = settings
	return layout, nil
}
