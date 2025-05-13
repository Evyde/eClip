package clipboard

import (
	"context"
	"time"
)

// ClipType defines the type of clipboard content.
type ClipType int

const (
	Text ClipType = iota
	Image
	File // For file paths
	// Potentially other types like HTML, RTF, etc.
)

// ClipItem represents a single item stored in the clipboard history.
type ClipItem struct {
	ID        string    // Unique identifier (e.g., hash of content or timestamp)
	Type      ClipType  // Type of the content
	Content   []byte    // Raw content data
	Timestamp time.Time // When the item was copied
	Source    string    // Origin of the clip (e.g., device name) - useful for sync
	FilePath  string    // Original file path, if Type is File
}

// Manager defines the interface for interacting with the system clipboard.
// Implementations will be platform-specific.
type Manager interface {
	// ReadText reads text content from the clipboard.
	ReadText(ctx context.Context) (string, error)
	// WriteText writes text content to the clipboard, optionally specifying the source.
	WriteText(ctx context.Context, text string, source string) error
	// ReadImage reads image content from the clipboard.
	ReadImage(ctx context.Context) ([]byte, error) // Returns raw image data (e.g., PNG bytes)
	// WriteImage writes image content to the clipboard, optionally specifying the source.
	WriteImage(ctx context.Context, imgData []byte, source string) error
	// ReadFiles reads file paths from the clipboard.
	ReadFiles(ctx context.Context) ([]string, error)
	// WriteFiles writes file paths to the clipboard, optionally specifying the source.
	WriteFiles(ctx context.Context, filePaths []string, source string) error
	// Monitor starts monitoring the clipboard for changes.
	// It sends new ClipItem data to the provided channel.
	Monitor(ctx context.Context, interval time.Duration) (<-chan ClipItem, error)
	// GetCurrentContent attempts to read the current clipboard content,
	// determines its type, and returns it as a ClipItem.
	GetCurrentContent(ctx context.Context) (*ClipItem, error)
}

// NewManager creates a new platform-specific clipboard manager.
// This function's implementation will be in platform-specific files
// using build constraints (e.g., clipboard_darwin.go, clipboard_windows.go).
func NewManager() (Manager, error) {
	// This function body will be provided by platform-specific files.
	// See clipboard_darwin.go and clipboard_windows.go
	return newManager()
}
