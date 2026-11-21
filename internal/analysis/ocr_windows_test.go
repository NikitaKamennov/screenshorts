//go:build windows && amd64

package analysis

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// This test starts the bundled OCR under the real Windows sandbox. It is
// opt-in and uses only the explicitly supplied synthetic test PNG/profile.
func TestWindowsBundledOCR(t *testing.T) {
	if os.Getenv("SCREENSHOOTER_WINDOWS_OCR_TESTS") != "1" { t.Skip("interactive Windows OCR integration is opt-in") }
	profile, input := os.Getenv("SCREENSHOOTER_TEST_RUNTIME"), os.Getenv("SCREENSHOOTER_TEST_PNG")
	if profile == "" || input == "" { t.Fatal("synthetic PNG and bundled runtime profile are required") }
	png, err := os.ReadFile(input)
	if err != nil { t.Fatal(err) }
	service := NewOCR(profile,t.TempDir(),NewQueue())
	if ready := service.Readiness(); !ready.Available { t.Fatal(ready.Reason) }
	for attempt:=0;attempt<2;attempt++ {
		ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
		text,err:=service.Recognize(ctx,func()([]byte,error){return png,nil},nil)
		cancel()
		if err!=nil { t.Fatalf("OCR attempt %d: %v",attempt+1,err) }
		lower:=strings.ToLower(text)
		for _,word:=range []string{"capture","тест","захвата","2026"} {
			if !strings.Contains(lower,word) { t.Fatalf("OCR attempt %d did not recognize %q in synthetic text: %q",attempt+1,word,text) }
		}
		t.Logf("OCR attempt %d recognized the Russian and English fixture",attempt+1)
	}
}
