//go:build windows

package clipboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings" // Ensure strings is imported
	"syscall"
	"time"
	"unsafe"

	"github.com/google/uuid" // Using UUID for unique IDs
	"golang.org/x/sys/windows"
)

// Windows API constants for clipboard formats
const (
	cfUnicodetext = 13 // Unicode text format
	// Add other formats like CF_BITMAP, CF_HDROP later
)

// Windows API functions (using golang.org/x/sys/windows where possible, syscall otherwise)
var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalFree   = kernel32.NewProc("GlobalFree")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procLstrcpyW     = kernel32.NewProc("lstrcpyW") // For copying wide strings
)

// windowsClipboardManager implements the Manager interface for Windows.
type windowsClipboardManager struct {
	lastContentHash string // Stores the hash of the last known content to detect changes
}

// newManager creates a new Windows clipboard manager.
func newManager() (Manager, error) {
	return &windowsClipboardManager{}, nil
}

// openClipboard opens the clipboard for access.
func openClipboard() error {
	// Retry mechanism for opening clipboard as it can be locked by other processes
	const maxRetries = 5
	const retryDelay = 100 * time.Millisecond
	var err error
	for i := 0; i < maxRetries; i++ {
		ret, _, callErr := procOpenClipboard.Call(0) // Pass 0 for hwndOwner
		if ret != 0 {                                // Success is non-zero
			return nil
		}
		err = callErr // Store the error
		time.Sleep(retryDelay)
	}
	return fmt.Errorf("failed to open clipboard after retries: %w", err)
}

// closeClipboard closes the clipboard.
func closeClipboard() error {
	ret, _, err := procCloseClipboard.Call()
	if ret == 0 { // Success is non-zero in docs, but often 0 means success for Close funcs? Check examples. Let's assume 0 is error.
		return fmt.Errorf("failed to close clipboard: %w", err)
	}
	return nil
}

// ReadText reads text from the Windows clipboard.
func (m *windowsClipboardManager) ReadText(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	if err := openClipboard(); err != nil {
		return "", fmt.Errorf("ReadText: %w", err)
	}
	defer closeClipboard()

	// Check if Unicode text format is available
	ret, _, _ := procIsClipboardFormatAvailable.Call(cfUnicodetext)
	if ret == 0 { // Format not available
		return "", nil // Treat as empty or non-text content
	}

	// Get clipboard data handle
	hData, _, err := procGetClipboardData.Call(cfUnicodetext)
	if hData == 0 {
		return "", fmt.Errorf("failed to get clipboard data: %w", err)
	}

	// Lock the global memory object
	pData, _, err := procGlobalLock.Call(hData)
	if pData == 0 {
		return "", fmt.Errorf("failed to lock clipboard data: %w", err)
	}
	defer procGlobalUnlock.Call(hData) // Unlock when done

	// Convert the pointer to a Go string (UTF-16 -> UTF-8)
	text := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(pData)))

	return text, nil
}

// WriteText writes text to the Windows clipboard.
func (m *windowsClipboardManager) WriteText(ctx context.Context, text string, source string) error {
	// source string is part of the interface but not directly used here for Windows API
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Convert Go string (UTF-8) to UTF-16 null-terminated slice
	utf16Text, err := windows.UTF16FromString(text)
	if err != nil {
		return fmt.Errorf("failed to convert text to UTF-16: %w", err)
	}
	size := uintptr(len(utf16Text) * 2) // Size in bytes (2 bytes per UTF-16 char)

	// Allocate global memory (GMEM_MOVEABLE allows the system to move the memory block)
	// GMEM_ZEROINIT is not strictly necessary but good practice
	const gmemMoveable = 0x0002
	hMem, _, err := procGlobalAlloc.Call(gmemMoveable, size)
	if hMem == 0 {
		return fmt.Errorf("failed to allocate global memory: %w", err)
	}

	// Lock the memory
	pMem, _, err := procGlobalLock.Call(hMem)
	if pMem == 0 {
		procGlobalFree.Call(hMem) // Free allocated memory on error
		return fmt.Errorf("failed to lock global memory: %w", err)
	}

	// Copy the UTF-16 text into the allocated memory
	// Use lstrcpyW for wide character string copy
	procLstrcpyW.Call(pMem, uintptr(unsafe.Pointer(&utf16Text[0])))

	// Unlock the memory
	procGlobalUnlock.Call(hMem) // Unlock before SetClipboardData

	// Open clipboard
	if err := openClipboard(); err != nil {
		procGlobalFree.Call(hMem) // Free allocated memory on error
		return fmt.Errorf("WriteText: %w", err)
	}
	defer closeClipboard()

	// Empty the clipboard
	ret, _, err := procEmptyClipboard.Call()
	if ret == 0 {
		procGlobalFree.Call(hMem)
		return fmt.Errorf("failed to empty clipboard: %w", err)
	}

	// Set the clipboard data
	ret, _, err = procSetClipboardData.Call(cfUnicodetext, hMem)
	if ret == 0 { // On failure, the system does not free hMem
		procGlobalFree.Call(hMem) // We must free it
		return fmt.Errorf("failed to set clipboard data: %w", err)
	}
	// On success, the system owns the memory (hMem), so we don't free it.

	// Update last known hash after writing
	newHash := calculateHash([]byte(text)) // Hash the original UTF-8 text
	m.lastContentHash = newHash

	return nil
}

// ReadImage is not implemented yet for Windows.
func (m *windowsClipboardManager) ReadImage(ctx context.Context) ([]byte, error) {
	// TODO: Implement using CF_BITMAP or CF_DIB format
	return nil, fmt.Errorf("ReadImage not implemented for windows yet")
}

// WriteImage is not implemented yet for Windows.
func (m *windowsClipboardManager) WriteImage(ctx context.Context, imgData []byte, source string) error {
	// source string is part of the interface but not directly used here
	// TODO: Implement using CF_BITMAP or CF_DIB format
	return fmt.Errorf("WriteImage not implemented for windows yet")
}

// ReadFiles is not implemented yet for Windows.
func (m *windowsClipboardManager) ReadFiles(ctx context.Context) ([]string, error) {
	// TODO: Implement using CF_HDROP format
	return nil, fmt.Errorf("ReadFiles not implemented for windows yet")
}

// WriteFiles is not implemented yet for Windows.
func (m *windowsClipboardManager) WriteFiles(ctx context.Context, filePaths []string, source string) error {
	// source string is part of the interface but not directly used here
	// TODO: Implement using CF_HDROP format
	return fmt.Errorf("WriteFiles not implemented for windows yet")
}

// Monitor starts polling the clipboard for changes on Windows.
func (m *windowsClipboardManager) Monitor(ctx context.Context, interval time.Duration) (<-chan ClipItem, error) {
	ch := make(chan ClipItem, 1) // Buffer of 1

	// Initialize lastContentHash
	initialItem, err := m.GetCurrentContent(ctx)
	if err != nil {
		fmt.Printf("Monitor: Error getting initial clipboard content: %v\n", err) // Replace with proper logging
		m.lastContentHash = ""
	} else if initialItem != nil {
		m.lastContentHash = calculateHash(initialItem.Content)
	} else {
		m.lastContentHash = "" // Empty clipboard initially
	}

	go func() {
		defer close(ch)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				fmt.Println("Monitor: Context cancelled, stopping.") // Replace with logger
				return
			case <-ticker.C:
				item, err := m.GetCurrentContent(ctx)
				if err != nil {
					// Don't stop monitoring on transient errors
					fmt.Printf("Monitor: Error reading clipboard: %v\n", err) // Replace with proper logging
					// Add specific error handling if needed (e.g., clipboard access errors)
					if err.Error() == "failed to open clipboard after retries" {
						// Maybe wait longer before next check if clipboard is busy
						time.Sleep(interval)
					}
					continue
				}

				if item == nil {
					// Clipboard is likely empty or contains unsupported type
					currentHash := ""
					if m.lastContentHash != currentHash {
						m.lastContentHash = currentHash
					}
					continue
				}

				currentHash := calculateHash(item.Content)
				if currentHash != m.lastContentHash {
					fmt.Printf("Monitor: Clipboard changed detected (Hash: %s)\n", currentHash) // Replace with logger
					m.lastContentHash = currentHash
					newItem := *item // Send a copy
					select {
					case ch <- newItem:
					case <-ctx.Done():
						fmt.Println("Monitor: Context cancelled while sending, stopping.") // Replace with logger
						return
					default:
						fmt.Println("Monitor: Warning - Channel buffer full, discarding change.") // Replace with logger
					}
				}
			}
		}
	}()

	return ch, nil
}

// GetCurrentContent attempts to read the current clipboard content on Windows.
// Currently only supports text.
func (m *windowsClipboardManager) GetCurrentContent(ctx context.Context) (*ClipItem, error) {
	hostname, err := os.Hostname()
	if err != nil {
		// Log the error and proceed with an empty hostname.
		// The peer_manager might fill it in later.
		fmt.Printf("GetCurrentContent: Warning: Failed to get hostname: %v\n", err) // Replace with proper logging
		hostname = ""                                                               // Default to empty if error
	}
	// 1. Try reading text
	text, err := m.ReadText(ctx)
	if err != nil {
		// Check for context cancellation first
		if err == context.Canceled || err == context.DeadlineExceeded {
			return nil, err
		}
		// Check if the error is due to clipboard access issues
		if _, ok := err.(syscall.Errno); !ok && !strings.Contains(err.Error(), "clipboard") {
			// If it's not a syscall error or a specific clipboard error message we added,
			// it might be something else worth reporting.
			fmt.Printf("GetCurrentContent: Non-clipboard error reading text: %v\n", err) // Replace with logger
		}
		// Otherwise, assume clipboard access failed or no text content.
		// Continue to check other types if implemented.
	}

	if text != "" {
		contentBytes := []byte(text) // Store as UTF-8
		return &ClipItem{
			ID:        generateID(),
			Type:      Text,
			Content:   contentBytes,
			Timestamp: time.Now(),
			Source:    hostname,
		}, nil
	}

	// 2. Try reading image (Not implemented)
	// imageData, imgErr := m.ReadImage(ctx)
	// if imgErr == nil && len(imageData) > 0 { ... }

	// 3. Try reading files (Not implemented)
	// filePaths, fileErr := m.ReadFiles(ctx)
	// if fileErr == nil && len(filePaths) > 0 { ... }

	// If no supported content type is found
	return nil, nil // Return nil, nil to indicate empty or unsupported content
}

// calculateHash generates a SHA256 hash for the given content.
// Using UTF-8 representation for hashing consistency across platforms.
func calculateHash(content []byte) string {
	hasher := sha256.New()
	hasher.Write(content)
	return hex.EncodeToString(hasher.Sum(nil))
}

// generateID creates a unique ID for a clip item.
func generateID() string {
	return uuid.NewString()
}
