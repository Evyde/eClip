//go:build !windows && !darwin

package clipboard

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// otherManager is a placeholder implementation for non-Windows/macOS systems.
type otherManager struct{}

func newManager() (Manager, error) {
	fmt.Println("Warning: Using placeholder clipboard manager for unsupported OS. Clipboard operations may not work.")
	return &otherManager{}, nil
}

func (m *otherManager) ReadText(ctx context.Context) (string, error) {
	return "", errors.New("clipboard ReadText not implemented for this OS")
}

func (m *otherManager) WriteText(ctx context.Context, text string, source string) error {
	return errors.New("clipboard WriteText not implemented for this OS")
}

func (m *otherManager) ReadImage(ctx context.Context) ([]byte, error) {
	return nil, errors.New("clipboard ReadImage not implemented for this OS")
}

func (m *otherManager) WriteImage(ctx context.Context, imgData []byte, source string) error {
	return errors.New("clipboard WriteImage not implemented for this OS")
}

func (m *otherManager) ReadFiles(ctx context.Context) ([]string, error) {
	return nil, errors.New("clipboard ReadFiles not implemented for this OS")
}

func (m *otherManager) WriteFiles(ctx context.Context, filePaths []string, source string) error {
	return errors.New("clipboard WriteFiles not implemented for this OS")
}

func (m *otherManager) Monitor(ctx context.Context, interval time.Duration) (<-chan ClipItem, error) {
	return nil, errors.New("clipboard Monitor not implemented for this OS")
}

func (m *otherManager) GetCurrentContent(ctx context.Context) (*ClipItem, error) {
	return nil, errors.New("clipboard GetCurrentContent not implemented for this OS")
}
