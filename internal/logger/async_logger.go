package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
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
	mu          sync.RWMutex
	logChan     chan string
	wg          sync.WaitGroup
	logLevel    LogLevel
	closed      bool
	callerDepth int // 新增调用栈深度控制
}

var (
	// Log is the global logger instance. It should be initialized by the application.
	// For example, in main.go:
	// logger.Log, err = logger.NewAsyncLogger("eclip.log", logger.LevelDebug)
	// if err != nil { log.Fatalf("Failed to initialize logger: %v", err) }
	// defer logger.Log.Close()
	Log *AsyncLogger
)

// 改进点1：增强调用信息获取
func getCallerInfo(depth int) (funcName, filePath string, line int) {
	pc, file, line, ok := runtime.Caller(depth)
	if !ok {
		return "unknown", "unknown", 0
	}

	// 解析函数名
	fn := runtime.FuncForPC(pc)
	funcName = "unknown"
	if fn != nil {
		funcName = filepath.Base(fn.Name())
	}

	// 简化文件路径
	if relPath, err := filepath.Rel(os.Getenv("GOPATH"), file); err == nil {
		file = relPath
	}

	return funcName, file, line
}

// NewAsyncLogger creates a new AsyncLogger instance.
// It logs messages to the specified file path.
// If filePath is empty, it logs to stderr.
func NewAsyncLogger(filePath string, level LogLevel) (*AsyncLogger, error) {
	l := &AsyncLogger{
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
		// 添加时间戳和协程ID
		// _, _, line := getCallerInfo(l.callerDepth)
		// goroutineID := fmt.Sprintf("Goroutine-%d", getGoroutineID())

		fmt.Printf("%s", msg)
	}
}

// log sends a message to the log channel if the level is sufficient.
// 改进点3：优化日志格式化
func (l *AsyncLogger) log(level LogLevel, format string, v ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.closed || level < l.logLevel {
		return
	}

	// 获取调用者信息（深度+2）
	funcName, file, line := getCallerInfo(3)
	var callerInfo string
	if level < LevelDebug {
		callerInfo = fmt.Sprintf("%s [%s:%d]", funcName, file, line)
	} else {
		callerInfo = funcName
	}

	// 组合完整消息
	fullMsg := fmt.Sprintf("[%s] %s %s\n",
		levelToString(level),
		callerInfo,
		fmt.Sprintf(format, v...))

	// 非阻塞写入（带超时保护）
	select {
	case l.logChan <- fullMsg:
	case <-time.After(50 * time.Millisecond):
		fmt.Printf("WARNING: Log queue overflow, dropped message: %s", fullMsg)
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

// Infof logs an info message.
func (l *AsyncLogger) Printf(format string, v ...interface{}) {
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
