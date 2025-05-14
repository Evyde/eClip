package main

import (
	"context" // Added for mDNS
	"eClip/internal/config"
	"eClip/internal/core/clipboard" // Will be temporarily commented out
	"eClip/internal/core/first_run"
	"eClip/internal/core/history"
	"eClip/internal/logger"
	"eClip/internal/network"      // Added for ClipboardServer
	"eClip/internal/network/mdns" // Changed from internal/core/sync
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings" // Added for log level parsing
	"syscall"
	"time" // Added for clipboard monitor interval

	"github.com/google/uuid"
)

var (
	// appLogger   *logger.AsyncLogger // Global logger instance - Replaced by logger.Log
	clipHistory *history.LRUCache // Global history cache instance
	peerManager *mdns.PeerManager // Declare peerManager here so it's in scope for the goroutine
)

// Helper function to convert string log level to logger.LogLevel
func parseLogLevel(levelStr string) logger.LogLevel {
	switch strings.ToLower(levelStr) {
	case "debug":
		return logger.LevelDebug
	case "info":
		return logger.LevelInfo
	case "warn":
		return logger.LevelWarn
	case "error":
		return logger.LevelError
	default:
		return logger.LevelInfo // Default to Info
	}
}

func main() {
	// 检查是否首次运行
	if first_run.IsFirstRun() {
		if err := first_run.Setup(); err != nil {
			panic("首次运行配置失败: " + err.Error())
		}
	}

	// 初始化配置
	if err := config.Init(); err != nil {
		panic("配置初始化失败: " + err.Error())
	}

	// 守护进程模式 (Temporarily commented out due to undefined functions)
	/*
		if !daemon.IsDaemon() {
			daemon.Daemonize()
			return
		}
	*/

	// 初始化日志系统
	var err error
	logCfg := config.GetConfig().Logging
	logLevel := parseLogLevel("DEBUG")                                 // parseLogLevel(logCfg.Level)
	logger.Log, err = logger.NewAsyncLogger(logCfg.FilePath, logLevel) // Initialize global logger.Log
	if err != nil {
		// 在 logger.Log 初始化失败时，我们不能使用 logger.Log 本身来记录错误
		// 因此，直接 panic 或使用标准 log 包
		log.Fatalf("全局日志系统初始化失败: %v", err) // 使用标准 log
		// panic("日志系统初始化失败: " + err.Error())
	}
	defer logger.Log.Close() // Close global logger.Log

	// 初始化历史存储
	historyCfg := config.GetConfig().History
	clipHistory, err = history.NewLRUCache(historyCfg.Capacity)
	if err != nil {
		logger.Log.Errorf("历史缓存初始化失败: %v", err) // Use global logger.Log
		panic("历史缓存初始化失败: " + err.Error())
	}
	// TODO: defer history.CloseDB() // Re-evaluate after checking internal/core/history/storage.go for DB operations

	// 启动剪切板监听
	clipManager, err := clipboard.NewManager()
	if err != nil {
		logger.Log.Errorf("剪切板管理器初始化失败: %v", err)
		panic("剪切板管理器初始化失败: " + err.Error())
	}

	monitorCtx, cancelMonitor := context.WithCancel(context.Background())
	defer cancelMonitor()

	// 从配置获取监控间隔，如果未配置则使用默认值
	var monitorInterval time.Duration
	// 假设配置路径为 config.GetConfig().Clipboard.MonitorIntervalSeconds
	// 由于我们不确定配置结构，暂时使用默认值
	// TODO: 从配置中读取剪切板监控间隔 config.GetConfig().Clipboard.MonitorIntervalSeconds
	monitorIntervalSeconds := 1 // 默认1秒
	monitorInterval = time.Duration(monitorIntervalSeconds) * time.Second

	// 初始化并注册 mDNS 服务
	instanceName := config.GetConfig().User.Username
	deviceName := config.GetConfig().User.DeviceName
	if deviceName == "" {
		deviceName = uuid.NewString()
	}
	txtRecords := []string{"app=eClip", "version=0.1.0", "id=" + deviceName}

	// Register mDNS service and get the listener
	mDNSServer, localServiceInfo, serviceListener, err := mdns.RegisterService(monitorCtx, instanceName, mdns.DefaultServiceType, txtRecords)
	if err != nil {
		logger.Log.Errorf("mDNS 服务注册失败: %v", err)
		panic("mDNS 服务注册失败: " + err.Error())
	}
	logger.Log.Infof("mDNS 服务注册成功: Instance: %s, Host: %s, Port: %d", localServiceInfo.Instance, localServiceInfo.HostName, localServiceInfo.Port)
	// Defer mDNS server shutdown. The listener will be closed by ClipboardServer's Stop method.
	defer func() {
		logger.Log.Infof("正在关闭 mDNS 服务...")
		mDNSServer.Shutdown()
		logger.Log.Infof("mDNS 服务已关闭.")
	}()

	// Initialize and start ClipboardServer using the listener from mDNS
	// localHostID can be localServiceInfo.HostName or os.Hostname()
	// Using localServiceInfo.HostName as it's consistent with mDNS registration
	clipboardServer, err := network.NewClipboardServer(clipManager, localServiceInfo.Port, localServiceInfo.HostName, serviceListener)
	if err != nil {
		logger.Log.Errorf("剪贴板服务器初始化失败: %v", err)
		serviceListener.Close() // Ensure listener is closed if server init fails
		panic("剪贴板服务器初始化失败: " + err.Error())
	}
	if err := clipboardServer.Start(monitorCtx); err != nil {
		logger.Log.Errorf("启动剪贴板服务器失败: %v", err)
		serviceListener.Close() // Ensure listener is closed if server start fails
		panic("启动剪贴板服务器失败: " + err.Error())
	}
	logger.Log.Infof("剪贴板服务器已在端口 %d 上启动", clipboardServer.Port())
	// Defer clipboard server stop. This will also close the listener.
	defer func() {
		logger.Log.Infof("正在关闭剪贴板服务器...")
		clipboardServer.Stop()
		logger.Log.Infof("剪贴板服务器已关闭.")
	}()

	// 初始化 PeerManager
	// PeerManager 的同步间隔可以从配置中读取
	peerManagerSyncInterval := 15 * time.Second // 从 30 秒减少到 15 秒
	// TODO: 从配置读取 PeerManager 同步间隔 config.GetConfig().Sync.DiscoveryIntervalSeconds

	// 使用声明的 peerManager 变量
	peerManager = mdns.NewPeerManager(localServiceInfo, clipManager, peerManagerSyncInterval)
	peerManager.StartDiscovery(monitorCtx, instanceName+"."+mdns.DefaultServiceType) // 使用 monitorCtx
	// peerManager.StartSyncServer(monitorCtx) // This line is removed as PeerManager no longer starts its own server.
	// Incoming clipboard data is handled by network.ClipboardServer.
	logger.Log.Infof("PeerManager 服务发现已启动。")
	defer peerManager.Stop() // 确保 PeerManager 在退出时停止

	// 在 PeerManager 初始化并启动后，再启动处理剪切板事件的 goroutine
	clipChannel, err := clipManager.Monitor(monitorCtx, monitorInterval)
	if err != nil {
		logger.Log.Errorf("启动剪切板监控失败: %v", err)
		// 根据应用策略决定是否 panic
	} else {
		logger.Log.Infof("剪切板监控已启动，间隔: %v", monitorInterval)
		go func() {
			for {
				select {
				case <-monitorCtx.Done():
					logger.Log.Infof("剪切板监控已停止.")
					return
				case item, ok := <-clipChannel:
					if !ok {
						logger.Log.Infof("剪切板监控通道已关闭.")
						return
					}
					logger.Log.Infof("新的剪切板项目: 类型: %d, 内容长度: %d, 源: %s", item.Type, len(item.Content), item.Source)
					// 将项目添加到历史记录
					if item.ID == "" {
						item.ID = fmt.Sprintf("%d", item.Timestamp.UnixNano())
					}
					clipHistory.Add(item)
					logger.Log.Debugf("剪切板项目已添加到历史: ID %s", item.ID)

					// 触发同步逻辑
					// 只有当剪贴板项目是本地产生（Source为空）或者非来自已知对等节点时才发送
					// 假设本地产生的 item.Source == ""
					// 并且 ClipboardServer 在接收到数据并调用 clipManager.SetText 时，会填充 Source 字段
					if peerManager != nil && (item.Source == "" || item.Source == localServiceInfo.HostName) { // 检查 Source 是否为本地
						logger.Log.Debugf("本地剪贴板更新 (源: '%s')，尝试通过 PeerManager 发送...", item.Source)
						peerManager.SendToPeers(item)
					} else if peerManager != nil {
						logger.Log.Debugf("剪贴板更新来自网络 (源: '%s')，不通过 PeerManager 再次发送.", item.Source)
					}
				}
			}
		}()
	}

	// 阻塞主线程，保持应用运行
	logger.Log.Infof("eClip 应用已启动。按 Ctrl+C 退出。")

	// 优雅地等待程序结束信号 (例如 SIGINT, SIGTERM)
	quitSignal := make(chan os.Signal, 1)
	signal.Notify(quitSignal, syscall.SIGINT, syscall.SIGTERM)
	<-quitSignal
	logger.Log.Infof("收到退出信号，正在关闭应用...")
	cancelMonitor() // 调用 cancelMonitor 会触发 monitorCtx.Done()
	// mDNSServer.Shutdown() // 已通过 defer 处理
	// peerManager.Stop() // 已通过 defer 处理
	// logger.Log.Close() // 已通过 defer 处理
	// history.CloseDB() // 如果有数据库操作，也需要关闭
}
