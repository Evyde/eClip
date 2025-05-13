package logger

import (
	"fmt"
	"log"
	"os"
	"sync"
)

// LogLevel defines the level of logging.
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

// AsyncLogger provides asynchronous logging capabilities.
type AsyncLogger struct {
	mu       sync.Mutex
	logger   *log.Logger
	logChan  chan string
	wg       sync.WaitGroup
	logLevel LogLevel
	closed   bool
}

var (
	// Log is the global logger instance. It should be initialized by the application.
	// For example, in main.go:
	// logger.Log, err = logger.NewAsyncLogger("eclip.log", logger.LevelDebug)
	// if err != nil { log.Fatalf("Failed to initialize logger: %v", err) }
	// defer logger.Log.Close()
	Log *AsyncLogger
)

// NewAsyncLogger creates a new AsyncLogger instance.
// It logs messages to the specified file path.
// If filePath is empty, it logs to stderr.
func NewAsyncLogger(filePath string, level LogLevel) (*AsyncLogger, error) {
	var output *os.File
	var err error

	if filePath != "" {
		output, err = os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
	} else {
		output = os.Stderr
	}

	l := &AsyncLogger{
		logger:   log.New(output, "", log.LstdFlags|log.Lshortfile),
		logChan:  make(chan string, 100), // Buffered channel
		logLevel: level,
	}

	l.wg.Add(1)
	go l.processLogs()

	return l, nil
}

// processLogs runs in a separate goroutine to handle log messages.
func (l *AsyncLogger) processLogs() {
	defer l.wg.Done()
	for msg := range l.logChan {
		l.logger.Output(3, msg) // Output with call depth 3 to get original caller file/line
	}
	// If the logger was associated with a file, close it.
	if closer, ok := l.logger.Writer().(*os.File); ok && closer != os.Stderr {
		closer.Close()
	}
}

// log sends a message to the log channel if the level is sufficient.
func (l *AsyncLogger) log(level LogLevel, format string, v ...interface{}) {
	l.mu.Lock()
	if l.closed || level < l.logLevel {
		l.mu.Unlock()
		return
	}
	l.mu.Unlock()

	// Format the message here to avoid doing it in the critical path if level is too low
	msg := fmt.Sprintf(format, v...)

	// Use non-blocking send to avoid blocking the caller if the channel is full
	select {
	case l.logChan <- fmt.Sprintf("[%s] %s", levelToString(level), msg):
	default:
		// Log channel is full, maybe log a warning synchronously?
		// Or just drop the message. For now, we drop it.
		log.Printf("WARNING: Log channel full, dropping message: %s", msg)
	}
}

// Debugf logs a debug message.
func (l *AsyncLogger) Debugf(format string, v ...interface{}) {
	l.log(LevelDebug, format, v...)
}

// Infof logs an info message.
func (l *AsyncLogger) Infof(format string, v ...interface{}) {
	l.log(LevelInfo, format, v...)
}

// Warnf logs a warning message.
func (l *AsyncLogger) Warnf(format string, v ...interface{}) {
	l.log(LevelWarn, format, v...)
}

// Errorf logs an error message.
func (l *AsyncLogger) Errorf(format string, v ...interface{}) {
	l.log(LevelError, format, v...)
}

// Close shuts down the logger gracefully.
func (l *AsyncLogger) Close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	l.mu.Unlock()

	close(l.logChan) // Close the channel to signal the processing goroutine
	l.wg.Wait()      // Wait for the processing goroutine to finish
}

// levelToString converts LogLevel to string representation.
func levelToString(level LogLevel) string {
	switch level {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}
