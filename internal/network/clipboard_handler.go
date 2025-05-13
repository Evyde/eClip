package network

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"eClip/internal/core/clipboard"
)

// ClipData represents the data structure for transferring clipboard content.
type ClipData struct {
	Type    clipboard.ClipType `json:"type"`
	Content []byte             `json:"content"`
	Source  string             `json:"source,omitempty"` // Optional: for identifying the origin
}

// ClipboardServer handles incoming clipboard data.
type ClipboardServer struct {
	listener    net.Listener
	clipManager clipboard.Manager
	// port field is kept for cases where listener is not provided directly,
	// though current flow provides listener from mDNS.
	port        int
	running     bool
	localHostID string // To identify the local machine and avoid self-processing
}

// NewClipboardServer creates a new ClipboardServer.
// It can optionally accept an existing net.Listener. If listener is nil,
// it will try to create one based on the port.
// localHostID can be os.Hostname() or similar.
func NewClipboardServer(clipManager clipboard.Manager, port int, localHostID string, listener net.Listener) (*ClipboardServer, error) {
	if clipManager == nil {
		return nil, fmt.Errorf("clipboard manager cannot be nil")
	}
	cs := &ClipboardServer{
		clipManager: clipManager,
		localHostID: localHostID,
		listener:    listener, // Store the provided listener
		port:        port,     // Store port, might be used if listener is nil or for info
	}
	if listener != nil {
		// If listener is provided, update port to match the listener's port
		if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
			cs.port = tcpAddr.Port
		}
	}
	return cs, nil
}

// Start begins listening for incoming clipboard data.
// If a listener was provided during construction, it uses that.
// Otherwise, it attempts to create a new listener on s.port.
func (s *ClipboardServer) Start(ctx context.Context) error {
	if s.listener == nil { // If no listener was provided, create one
		if s.port == 0 {
			// Attempt to listen on a dynamic port if port is 0 and no listener given
			// This case might need more robust handling or clearer contract
			// For now, assume port is set if listener is nil, or mDNS provides listener
			return fmt.Errorf("port must be set if no listener is provided and dynamic port is not explicitly handled here")
		}
		lc := net.ListenConfig{}
		var err error
		s.listener, err = lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", s.port))
		if err != nil {
			return fmt.Errorf("failed to listen on port %d: %w", s.port, err)
		}
		log.Printf("Clipboard server created new listener on port %d\n", s.port)
	} else {
		log.Printf("Clipboard server using provided listener on port %d\n", s.Port())
	}

	s.running = true
	go s.acceptConnections(ctx)
	return nil
}

func (s *ClipboardServer) acceptConnections(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Println("Clipboard server shutting down...")
			s.Stop()
			return
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				if s.running { // Avoid logging errors if server was intentionally stopped
					log.Printf("Failed to accept connection: %v\n", err)
				}
				// If accept fails, check if context is done before continuing loop
				select {
				case <-ctx.Done():
					continue // Exit loop if context is done
				default:
					// Add a small delay to prevent tight loop on persistent errors
					time.Sleep(100 * time.Millisecond)
					continue
				}
			}
			log.Printf("Accepted connection from %s\n", conn.RemoteAddr().String())
			go s.handleConnection(ctx, conn)
		}
	}
}

func (s *ClipboardServer) handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	decoder := json.NewDecoder(reader)

	var data ClipData
	if err := decoder.Decode(&data); err != nil {
		if err == io.EOF {
			log.Printf("Connection closed by peer %s before sending data.\n", conn.RemoteAddr().String())
			return
		}
		log.Printf("Failed to decode clipboard data from %s: %v\n", conn.RemoteAddr().String(), err)
		return
	}

	// Optional: Check if the data came from this machine to avoid loops,
	// if Source field is reliably set by clients.
	if data.Source == s.localHostID {
		log.Printf("Ignoring clipboard data from self (%s)\n", data.Source)
		return
	}

	log.Printf("Received clipboard data from %s: Type %v, Size %d bytes\n", conn.RemoteAddr().String(), data.Type, len(data.Content))

	switch data.Type {
	case clipboard.Text:
		// Pass the original source of the data to WriteText
		err := s.clipManager.WriteText(ctx, string(data.Content), data.Source)
		if err != nil {
			log.Printf("Failed to write text to clipboard: %v\n", err)
		} else {
			log.Println("Successfully wrote received text to clipboard.")
		}
	case clipboard.Image:
		// TODO: Implement image handling
		// err := s.clipManager.WriteImage(ctx, data.Content, data.Source)
		// if err != nil {
		// 	log.Printf("Failed to write image to clipboard: %v", err)
		// }
		log.Println("Received image data, but image handling is not yet implemented.")
	case clipboard.File:
		// TODO: Implement file handling
		// For security, be very careful with writing files or file paths.
		// May need to deserialize []string from data.Content
		// err := s.clipManager.WriteFiles(ctx, filePaths, data.Source) // Assuming filePaths are extracted
		log.Println("Received file data, but file handling is not yet implemented.")
	default:
		log.Printf("Received unknown clipboard data type: %v\n", data.Type)
	}
}

// Stop gracefully shuts down the clipboard server.
func (s *ClipboardServer) Stop() {
	s.running = false
	if s.listener != nil {
		log.Println("Closing clipboard server listener...")
		s.listener.Close()
	}
}

// Port returns the port the server is listening on.
func (s *ClipboardServer) Port() int {
	if s.listener == nil {
		return 0 // Or s.port if it was pre-assigned
	}
	return s.listener.Addr().(*net.TCPAddr).Port
}

// IsRunning checks if the server is currently running.
func (s *ClipboardServer) IsRunning() bool {
	return s.running
}
