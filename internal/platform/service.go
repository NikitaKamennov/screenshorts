package platform

import (
	"context"
	"errors"
	"skrinpolka/internal/core"
)

var ErrCancelled = errors.New("Захват отменён")
var ErrUnavailable = errors.New("Возможность недоступна в текущем окружении")

type Actions struct {
	Capture func()
	CaptureScreen func()
	CaptureSilent func()
	CaptureSilentArea func()
	OpenLibrary func()
	OpenSettings func()
	Quit func()
	Changed func(Capabilities)
}

type Service interface {
	Capabilities() Capabilities
	CaptureArea(context.Context) (Capture, error)
	CaptureScreen(context.Context) (Capture, error)
	CaptureSilent(context.Context) (Capture, error)
	CaptureRegion(context.Context, core.ScreenRegion) (Capture, error)
	SetShortcuts(context.Context, Shortcuts) (Shortcuts, error)
	Close() error
}
