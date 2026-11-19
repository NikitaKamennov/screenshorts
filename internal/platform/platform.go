// Package platform provides desktop capabilities and native operating-system
// adapters. Detect is the initial state before the service has been started.
package platform

import (
	"os"
	"runtime"
	"skrinpolka/internal/core"
)

type Feature struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

type Capabilities struct {
	OS              string  `json:"os"`
	Session         string  `json:"session"`
	Tray            Feature `json:"tray"`
	GlobalShortcut  Feature `json:"global_shortcut"`
	GlobalScreenShortcut Feature `json:"global_screen_shortcut"`
	GlobalSilentShortcut Feature `json:"global_silent_shortcut"`
	GlobalSilentAreaShortcut Feature `json:"global_silent_area_shortcut"`
	AreaCapture     Feature `json:"area_capture"`
	ScreenCapture   Feature `json:"screen_capture"`
	SilentCapture   Feature `json:"silent_capture"`
	ImageClipboard  Feature `json:"image_clipboard"`
	Shortcut        string  `json:"shortcut"`
	ScreenShortcut  string  `json:"screen_shortcut"`
	SilentShortcut  string  `json:"silent_shortcut"`
	SilentAreaShortcut string `json:"silent_area_shortcut"`
}

// Detect reports implemented capabilities, not what the operating system might
// support after a future adapter is added.
func Detect() Capabilities {
	pending := Feature{Available: false, Reason: "Системные адаптеры запускаются"}
	session := "desktop"
	if runtime.GOOS == "linux" {
		session = os.Getenv("XDG_SESSION_TYPE")
		if session == "" {
			session = "unknown"
		}
	}
	return Capabilities{
		OS: runtime.GOOS, Session: session,
		Tray: pending, GlobalShortcut: pending, GlobalScreenShortcut: pending, AreaCapture: pending, ScreenCapture: pending,
		GlobalSilentShortcut: pending, SilentCapture: pending,
		GlobalSilentAreaShortcut: pending,
		ImageClipboard: pending,
	}
}

type Capture struct {
	PNG           []byte
	MonitorID     string
	Scale         float64
	Region        *core.ScreenRegion
}

// Unavailable is a terminal startup failure, distinct from Detect's pending
// state. It lets the library stay open without claiming adapters are loading.
func Unavailable(reason string) Capabilities {
	value := Detect()
	feature := Feature{Reason:reason}
	value.Tray, value.AreaCapture, value.ScreenCapture = feature, feature, feature
	value.GlobalShortcut, value.GlobalScreenShortcut = feature, feature
	value.GlobalSilentShortcut, value.GlobalSilentAreaShortcut = feature, feature
	value.SilentCapture, value.ImageClipboard = feature, feature
	return value
}
