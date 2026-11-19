// Package analysis provides local OCR and background removal through a shared
// protected worker queue, and reserves the protocol for future CPU adapters.
package analysis

import "context"

const Protocol = "skrinpolka-worker-v1"

type Operation string

const (
	OCR              Operation = "ocr"
	EmbedText        Operation = "embed_text"
	RemoveBackground Operation = "remove_background"
)

type Budget struct {
	Threads       int   `json:"threads"`
	MemoryBytes   int64 `json:"memory_bytes"`
	TimeoutSeconds int  `json:"timeout_seconds"`
}

// ImagePath must refer to the current flattened image (saved or draft), never a raw capture
// containing pixels hidden by later redaction. The host validates this path.
type Request struct {
	Protocol    string    `json:"protocol"`
	TaskID      string    `json:"task_id"`
	ScreenshotID string   `json:"screenshot_id"`
	ImageSHA256 string    `json:"image_sha256"`
	ImagePath   string    `json:"image_path,omitempty"`
	Text        string    `json:"text,omitempty"`
	Operation   Operation `json:"operation"`
	ModelID     string    `json:"model_id"`
	Budget      Budget    `json:"budget"`
}

type Result struct {
	Protocol    string    `json:"protocol"`
	TaskID      string    `json:"task_id"`
	ImageSHA256 string    `json:"image_sha256"`
	ModelID     string    `json:"model_id"`
	ModelRevision string  `json:"model_revision"`
	Text        string    `json:"text,omitempty"`
	Vector      []float32 `json:"vector,omitempty"`
	OutputPath  string    `json:"output_path,omitempty"`
}

type Worker interface {
	Process(context.Context, Request) (Result, error)
}
