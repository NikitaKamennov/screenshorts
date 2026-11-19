//go:build windows

package platform

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"runtime"
	"testing"
	"time"
)

// These integration tests access the interactive Windows desktop. Keep them
// opt-in so normal package tests do not capture a user's screen or take focus.
func requireWindowsDesktop(t *testing.T) {
	t.Helper()
	if os.Getenv("SCREENSHORTS_WINDOWS_NATIVE_TESTS") != "1" {
		t.Skip("set SCREENSHORTS_WINDOWS_NATIVE_TESTS=1 for interactive Windows capture")
	}
}

func TestWindowsFrameReadback(t *testing.T) {
	requireWindowsDesktop(t)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ConfigureProcess()
	frame, err := readWindowsFrame()
	if err != nil { t.Fatal(err) }
	defer frame.close()
	if len(frame.pixels) != frame.width*frame.height*4 {
		t.Fatalf("incomplete frame buffer: %d bytes for %dx%d",len(frame.pixels),frame.width,frame.height)
	}
}

func TestWindowsCaptureAfterCancelledRequest(t *testing.T) {
	requireWindowsDesktop(t)
	ConfigureProcess()
	service, err := New(Actions{})
	if err != nil { t.Fatal(err) }
	defer func() { if err := service.Close(); err != nil { t.Error(err) } }()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.CaptureScreen(cancelled); err == nil { t.Fatal("cancelled request unexpectedly succeeded") }
	for attempt := 0; attempt < 3; attempt++ {
		ctx, done := context.WithTimeout(context.Background(),5*time.Second)
		capture, err := service.CaptureScreen(ctx)
		done()
		if err != nil { t.Fatalf("capture %d after cancellation: %v",attempt+1,err) }
		config, err := png.DecodeConfig(bytes.NewReader(capture.PNG))
		if err != nil || config.Width <= 0 || config.Height <= 0 { t.Fatalf("capture %d returned invalid PNG: %v",attempt+1,err) }
	}
}
