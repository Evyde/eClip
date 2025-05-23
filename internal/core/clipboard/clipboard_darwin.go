//go:build darwin

package clipboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid" // Using UUID for unique IDs
)

func init() {
	getClipboardText = darwinGetClipboardText
	setClipboardText = darwinSetClipboardText
}

// darwinClipboardManager implements the Manager interface for macOS.
type darwinClipboardManager struct {
	lastContentHash string // Stores the hash of the last known content to detect changes
}

// newManager creates a new macOS clipboard manager.
// This function is called by the generic NewManager in clipboard.go
func newManager() (Manager, error) {
	return &darwinClipboardManager{}, nil
}

// GetText retrieves text from clipboard. This method implements the Manager interface.
func (m *darwinClipboardManager) GetText() (string, error) {
	// This implementation uses the darwinGetClipboardText function,
	// which is consistent with how platform-specific functions are set up.
	text, err := darwinGetClipboardText()
	if err != nil {
		// pbpaste exits with an error if the clipboard is empty or doesn't contain text.
		// We treat this as empty content rather than a fatal error.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil // No text content or empty
		}
		return "", fmt.Errorf("GetText (darwin): %w", err)
	}
	return text, nil
}

// SetText sets text to clipboard. This method implements the Manager interface.
func (m *darwinClipboardManager) SetText(text string, source string) error {
	// source string is part of the interface but not directly used here for pbcopy
	// This implementation uses the darwinSetClipboardText function.
	err := darwinSetClipboardText(text)
	if err != nil {
		return fmt.Errorf("SetText (darwin): %w", err)
	}
	// Update last known hash after writing
	newHash := calculateHash([]byte(text))
	m.lastContentHash = newHash
	return nil
}

// ReadImage reads image data (PNG) from the macOS clipboard using osascript.
func (m *darwinClipboardManager) ReadImage(ctx context.Context) ([]byte, error) {
	// AppleScript to get PNG data from clipboard.
	// It tries to get data as 'PNGf'. If not found, it returns an error.
	// osascript writes the raw PNG data to stdout if successful.
	script := `
		set imageData to ""
		try
			set imageData to (the clipboard as «class PNGf»)
		on error
			return "" -- Return empty string to indicate no PNG data or error
		end try
		return imageData
	`
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	output, err := cmd.Output()
	if err != nil {
		// Check if context was cancelled
		if ctx.Err() == context.Canceled || ctx.Err() == context.DeadlineExceeded {
			return nil, ctx.Err()
		}
		// osascript might exit with an error if clipboard doesn't contain PNG
		// or if the script itself has an issue.
		// A non-zero exit code from osascript often means the script errored (e.g., no image).
		// Output might be empty or contain error messages from osascript.
		// We consider any error here as "no image data found" or "failed to read".
		return nil, fmt.Errorf("failed to execute osascript for ReadImage or no PNG image on clipboard: %w, output: %s", err, string(output))
	}

	if len(output) == 0 {
		return nil, nil // No PNG data on clipboard
	}

	return output, nil
}

// WriteImage is not implemented yet for macOS using pbpaste/pbcopy.
func (m *darwinClipboardManager) WriteImage(ctx context.Context, imgData []byte, source string) error {
	// source string is part of the interface but not directly used here
	// TODO: Implement using NSPasteboard or specific image commands
	// This would likely involve writing imgData to a temp file and using osascript
	// to tell an application to copy it, or using more complex AppleScript to handle raw data.
	return fmt.Errorf("WriteImage not implemented for darwin yet")
}

// ReadFiles reads file URLs from the macOS clipboard using osascript.
func (m *darwinClipboardManager) ReadFiles(ctx context.Context) ([]string, error) {
	// AppleScript to get file URLs from clipboard.
	// It checks for 'file URL' and tries to coerce to a list of strings.
	script := `
		set outputList to {}
		try
			set clipboardData to the clipboard as record
			if clipboardData contains «class furl» then
				set fileOrFiles to (the clipboard as «class furl»)
				if class of fileOrFiles is list then
					repeat with aFile in fileOrFiles
						set end of outputList to POSIX path of aFile
					end repeat
				else
					set end of outputList to POSIX path of fileOrFiles
				end if
			else if clipboardData contains «class kfurl» then -- kFilenamesPboardType (deprecated but might appear)
                 set fileOrFiles to (the clipboard as «class kfurl»)
                 if class of fileOrFiles is list then
                     repeat with aFile in fileOrFiles
                         set end of outputList to POSIX path of aFile
                     end repeat
                 else
                     set end of outputList to POSIX path of fileOrFiles
                 end if
            else if (clipboard info) contains "public.file-url" then
				-- More robust check for file URLs, might require more complex parsing if not directly list of URLs
				-- This part might need refinement based on how different apps put file URLs.
				-- For now, assume it's a list of file URLs or a single one.
				set theClipboardContent to the clipboard
				if class of theClipboardContent is text then -- Sometimes it's just text containing file:// URLs
					if theClipboardContent starts with "file://" then
						-- This is a naive way to handle single file path string.
						-- A proper solution would parse multiple URLs if they are newline separated.
						set end of outputList to text 7 thru -1 of theClipboardContent -- Crude way to strip "file://"
					end if
				else
					-- Attempt to get as list of POSIX paths if possible
					-- This is a fallback and might not always work.
					-- A more robust solution would use NSPasteboard's API through ObjC bridge or similar.
					try
						set posixFiles to {}
						tell application "System Events"
							set posixFiles to path of (get the clipboard as «class furl»)
						end tell
						if class of posixFiles is list then
							set outputList to posixFiles
						else
							set end of outputList to posixFiles
						end if
					end try
				end if
			end if
		on error errMsg number errNum
			-- Return empty on error, indicating no files or issue
			return {}
		end try
		return outputList
	`
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.Canceled || ctx.Err() == context.DeadlineExceeded {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("failed to execute osascript for ReadFiles: %w, output: %s", err, string(output))
	}

	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" {
		return nil, nil // No file URLs found
	}

	// osascript returns comma-separated list for list results
	// e.g., "/path/to/file1, /path/to/file2"
	paths := strings.Split(outputStr, ", ")
	cleanedPaths := make([]string, 0, len(paths))
	for _, p := range paths {
		trimmedPath := strings.TrimSpace(p)
		if trimmedPath != "" {
			// Further clean file paths if they are URI encoded or have "file://" prefix
			// For POSIX paths from AppleScript, they should be clean.
			// If script returns file URLs like "file:///path/to/file", need to strip "file://"
			if strings.HasPrefix(trimmedPath, "file://") {
				trimmedPath = strings.TrimPrefix(trimmedPath, "file://")
				// Decode URI encoding if present, though POSIX path from AppleScript should be decoded.
				// Example: path, _ = url.PathUnescape(trimmedPath)
			}
			cleanedPaths = append(cleanedPaths, trimmedPath)
		}
	}

	if len(cleanedPaths) == 0 {
		return nil, nil
	}
	return cleanedPaths, nil
}

// WriteFiles is not implemented yet for macOS using pbpaste/pbcopy.
func (m *darwinClipboardManager) WriteFiles(ctx context.Context, filePaths []string, source string) error {
	// source string is part of the interface but not directly used here
	// TODO: Implement using NSPasteboard
	return fmt.Errorf("WriteFiles not implemented for darwin yet")
}

// Monitor starts polling the clipboard for changes on macOS.
func (m *darwinClipboardManager) Monitor(ctx context.Context, interval time.Duration) (<-chan Item, error) {
	ch := make(chan Item, 1) // Buffer of 1 to avoid blocking on send

	// Initialize lastContentHash with current content
	initialItem, err := m.GetCurrentContent(ctx)
	if err != nil {
		// Log or handle initial read error? For now, proceed with empty hash.
		fmt.Printf("Monitor: Error getting initial clipboard content: %v\n", err) // Replace with proper logging
		m.lastContentHash = ""
	} else if initialItem != nil {
		m.lastContentHash = calculateHash([]byte(initialItem.Content)) // Ensure content is []byte for hash
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
				// fmt.Println("Monitor: Checking clipboard...") // Debug log
				item, err := m.GetCurrentContent(ctx)
				if err != nil {
					// Don't stop monitoring on transient errors, just log.
					fmt.Printf("Monitor: Error reading clipboard: %v\n", err) // Replace with proper logging
					continue
				}

				if item == nil {
					// Clipboard is likely empty or contains unsupported type
					currentHash := ""
					if m.lastContentHash != currentHash {
						// fmt.Println("Monitor: Clipboard cleared or unsupported type.") // Debug log
						m.lastContentHash = currentHash
						// Optionally send an "empty" signal? For now, do nothing.
					}
					continue
				}

				currentHash := calculateHash([]byte(item.Content)) // Ensure content is []byte for hash
				// fmt.Printf("Monitor: Current Hash: %s, Last Hash: %s\n", currentHash, m.lastContentHash) // Debug log

				if currentHash != m.lastContentHash {
					fmt.Printf("Monitor: Clipboard changed detected (Hash: %s)\n", currentHash) // Replace with logger
					m.lastContentHash = currentHash
					// Send a copy to avoid race conditions if item is modified later
					newItem := *item
					select {
					case ch <- newItem:
						// fmt.Println("Monitor: Sent item to channel.") // Debug log
					case <-ctx.Done():
						fmt.Println("Monitor: Context cancelled while sending, stopping.") // Replace with logger
						return
					default:
						// Should not happen with buffered channel unless receiver is slow/stuck
						fmt.Println("Monitor: Warning - Channel buffer full, discarding change.") // Replace with logger
					}
				}
			}
		}
	}()

	return ch, nil
}

// GetCurrentContent attempts to read the current clipboard content on macOS.
func (m *darwinClipboardManager) GetCurrentContent(ctx context.Context) (*Item, error) {
	hostname, err := os.Hostname()
	if err != nil {
		fmt.Printf("GetCurrentContent: Warning: Failed to get hostname: %v\n", err) // Replace with proper logging
		hostname = ""
	}

	// 1. Try reading text
	// Use the interface method GetText() which now calls darwinGetClipboardText
	text, textErr := m.GetText() // Removed ctx as GetText() doesn't take it
	if textErr != nil {
		// Context cancellation check might be less relevant here if GetText doesn't use ctx
		// if textErr == context.Canceled || textErr == context.DeadlineExceeded {
		// 	return nil, textErr
		// }
		fmt.Printf("GetCurrentContent: Info: Error reading text (might be non-text content): %v\n", textErr)
	}

	if text != "" {
		return &Item{
			ID:        generateID(),
			Type:      TypeText, // Use ItemType const
			Content:   text,     // Content is string
			Timestamp: time.Now(),
			Source:    hostname,
		}, nil
	}

	// 2. Try reading image
	imageData, imgErr := m.ReadImage(ctx) // ReadImage still uses context
	if imgErr != nil {
		if imgErr == context.Canceled || imgErr == context.DeadlineExceeded {
			return nil, imgErr
		}
		// fmt.Printf("GetCurrentContent: Info: Error reading image: %v\n", imgErr)
	}

	if len(imageData) > 0 {
		// Item.Content is string. Convert imageData (byte slice) to string.
		// For binary data, base64 encoding is common. For simplicity, using string() for now,
		// but this might not be ideal for all binary data.
		return &Item{
			ID:        generateID(),
			Type:      TypeImage,         // Use ItemType const
			Content:   string(imageData), // TODO: Consider base64 for binary data
			Timestamp: time.Now(),
			Source:    hostname,
		}, nil
	}

	// 3. Try reading files
	filePaths, fileErr := m.ReadFiles(ctx) // ReadFiles still uses context
	if fileErr != nil {
		if fileErr == context.Canceled || fileErr == context.DeadlineExceeded {
			return nil, fileErr
		}
		// fmt.Printf("GetCurrentContent: Info: Error reading files: %v\n", fileErr)
	}

	if len(filePaths) > 0 {
		filePath := filePaths[0]
		fileContentBytes, err := readFileContent(filePath)
		if err != nil {
			fmt.Printf("GetCurrentContent: Error reading file content for %s: %v\n", filePath, err)
			return nil, fmt.Errorf("failed to read content of file %s: %w", filePath, err)
		}
		// Item.Content is string. Convert fileContentBytes to string.
		return &Item{
			ID:        generateID(),
			Type:      TypeFile,                 // Use ItemType const
			Content:   string(fileContentBytes), // TODO: Consider base64 for binary data. Store path in Item?
			Timestamp: time.Now(),
			Source:    hostname,
			// FilePath:  filePath, // Item struct doesn't have FilePath. Store in Content or extend Item.
		}, nil
	}

	return nil, nil
}

// readFileContent reads the content of a file given its path.
func readFileContent(filePath string) ([]byte, error) {
	// Basic security check: ensure path is not trying to escape expected locations,
	// or ensure it's an absolute path. For now, assume filePath is valid.
	// In a real app, more path validation would be needed.
	cleanPath := strings.TrimSpace(filePath)
	if cleanPath == "" {
		return nil, fmt.Errorf("file path is empty")
	}

	// Using os.ReadFile is generally preferred over exec.Command("cat", ...)
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", cleanPath, err)
	}
	return data, nil
}

// calculateHash generates a SHA256 hash for the given content.
func calculateHash(content []byte) string {
	hasher := sha256.New()
	hasher.Write(content)
	return hex.EncodeToString(hasher.Sum(nil))
}

// generateID creates a unique ID for a clip item.
func generateID() string {
	return uuid.NewString()
}

func darwinGetClipboardText() (string, error) {
	cmd := exec.Command("pbpaste")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pbpaste failed: %w", err)
	}
	return string(output), nil
}

func darwinSetClipboardText(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
