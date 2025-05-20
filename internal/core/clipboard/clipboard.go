package clipboard

import (
	"context"
	"fmt"
	"time"
)

// ItemType represents the type of clipboard content
type ItemType int

const (
	TypeText ItemType = iota
	TypeImage
	TypeFile
)

// Item represents a clipboard item
type Item struct {
	ID        string
	Type      ItemType
	Content   string
	Timestamp time.Time
	Source    string
}

// Manager interface defines clipboard operations
type Manager interface {
	GetText() (string, error)
	SetText(text string, source string) error
	Monitor(ctx context.Context, interval time.Duration) (<-chan Item, error)
}

// clipboardManager implements the Manager interface
type clipboardManager struct {
	lastContent string
}

// NewManager creates a new clipboard manager
func NewManager() (Manager, error) {
	return &clipboardManager{}, nil
}

// GetText retrieves text from clipboard
func (m *clipboardManager) GetText() (string, error) {
	// Platform-specific implementation
	text, err := getClipboardText()
	if err != nil {
		return "", fmt.Errorf("获取剪贴板内容失败: %w", err)
	}
	return text, nil
}

// SetText sets text to clipboard
func (m *clipboardManager) SetText(text string, source string) error {
	// Platform-specific implementation
	err := setClipboardText(text)
	if err != nil {
		return fmt.Errorf("设置剪贴板内容失败: %w", err)
	}
	m.lastContent = text
	return nil
}

// Monitor starts monitoring clipboard changes
func (m *clipboardManager) Monitor(ctx context.Context, interval time.Duration) (<-chan Item, error) {
	items := make(chan Item, 1)

	go func() {
		defer close(items)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var lastContent string
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				content, err := m.GetText()
				if err != nil {
					continue
				}

				if content != lastContent {
					lastContent = content
					item := Item{
						ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
						Type:      TypeText,
						Content:   content,
						Timestamp: time.Now(),
						Source:    "", // 确保本地更改的Source为空
					}
					items <- item
				}
			}
		}
	}()

	return items, nil
}

// Platform-specific implementations
// These functions should be implemented in clipboard_windows.go and clipboard_darwin.go

var (
	getClipboardText func() (string, error)
	setClipboardText func(text string) error
)
