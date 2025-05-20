package mdns

import (
	"context"
	"eClip/internal/core/clipboard"
	"eClip/internal/logger"
	"fmt"
	"net"
	"sync"
	"time"
)

// PeerManager 管理网络上的对等节点
type PeerManager struct {
	localInfo        *ServiceInfo
	clipManager      *clipboard.Manager
	knownPeers       map[string]*Peer
	syncInterval     time.Duration
	mu               sync.RWMutex
	discoveryRunning bool
	interfaces       []net.Interface
	verboseLogging   bool
}

// Peer 表示网络上的一个对等节点
type Peer struct {
	ServiceInfo *ServiceInfo
	LastSeen    time.Time
	Active      bool
}

// NewPeerManager 创建一个新的对等节点管理器
func NewPeerManager(localInfo *ServiceInfo, clipManager *clipboard.Manager, syncInterval time.Duration, interfaces []net.Interface) *PeerManager {
	return &PeerManager{
		localInfo:    localInfo,
		clipManager:  clipManager,
		knownPeers:   make(map[string]*Peer),
		syncInterval: syncInterval,
		interfaces:   interfaces,
	}
}

// SetVerboseLogging 设置是否启用详细日志记录
func (pm *PeerManager) SetVerboseLogging(verbose bool) {
	pm.verboseLogging = verbose
}

// StartDiscovery 启动服务发现
func (pm *PeerManager) StartDiscovery(ctx context.Context, serviceType string) {
	pm.mu.Lock()
	if pm.discoveryRunning {
		pm.mu.Unlock()
		return
	}
	pm.discoveryRunning = true
	pm.mu.Unlock()

	// 开始监听 mDNS 事件
	go func() {
		// 创建浏览器，特别指定使用的网络接口
		browser, err := NewBrowser(serviceType, pm.interfaces)
		if err != nil {
			logger.Log.Errorf("创建mDNS浏览器失败: %v", err)
			return
		}
		defer browser.Close()

		entryChannel := browser.Browse(ctx)

		for {
			select {
			case <-ctx.Done():
				logger.Log.Infof("服务发现停止.")
				return
			case entry, ok := <-entryChannel:
				if !ok {
					logger.Log.Infof("发现通道已关闭.")
					return
				}

				// 跳过本地服务
				if entry.Instance == pm.localInfo.Instance && entry.HostName == pm.localInfo.HostName {
					if pm.verboseLogging {
						logger.Log.Debugf("跳过本地服务: %s@%s", entry.Instance, entry.HostName)
					}
					continue
				}

				// 解析主机地址
				var ipAddrs []net.IP
				for _, addr := range entry.AddrIPv4 {
					if !addr.IsUnspecified() && !addr.IsLoopback() {
						ipAddrs = append(ipAddrs, addr)
					}
				}
				for _, addr := range entry.AddrIPv6 {
					if !addr.IsUnspecified() && !addr.IsLoopback() {
						ipAddrs = append(ipAddrs, addr)
					}
				}

				if len(ipAddrs) == 0 {
					if pm.verboseLogging {
						logger.Log.Warnf("发现的服务没有有效IP地址: %s@%s", entry.Instance, entry.HostName)
					}
					continue
				}

				// 创建或更新对等点
				pm.mu.Lock()
				peer, exists := pm.knownPeers[entry.HostName]
				if !exists {
					peer = &Peer{
						ServiceInfo: entry,
						LastSeen:    time.Now(),
						Active:      true,
					}
					pm.knownPeers[entry.HostName] = peer
					logger.Log.Infof("发现新对等点: %s@%s (IP: %v, 端口: %d)",
						entry.Instance, entry.HostName, ipAddrs, entry.Port)
				} else {
					peer.LastSeen = time.Now()
					peer.Active = true
					peer.ServiceInfo = entry // 更新服务信息
					if pm.verboseLogging {
						logger.Log.Debugf("更新已知对等点: %s@%s (IP: %v, 端口: %d)",
							entry.Instance, entry.HostName, ipAddrs, entry.Port)
					}
				}
				pm.mu.Unlock()
			}
		}
	}()

	// 清理过期的对等点
	go func() {
		ticker := time.NewTicker(pm.syncInterval * 2)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now()
				pm.mu.Lock()
				for hostName, peer := range pm.knownPeers {
					// 超过3个检查间隔未见，标记为非活动
					if now.Sub(peer.LastSeen) > pm.syncInterval*6 {
						if peer.Active {
							logger.Log.Infof("对等点变为非活动状态: %s@%s", peer.ServiceInfo.Instance, hostName)
							peer.Active = false
						}
					}
				}
				pm.mu.Unlock()
			}
		}
	}()
}

// SendToPeers 向所有活动对等点发送剪贴板项目
func (pm *PeerManager) SendToPeers(item interface{}) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for hostName, peer := range pm.knownPeers {
		if !peer.Active {
			if pm.verboseLogging {
				logger.Log.Debugf("跳过向非活动对等点发送: %s", hostName)
			}
			continue
		}

		// 尝试获取适当的IP地址
		var targetAddr string
		for _, addr := range peer.ServiceInfo.AddrIPv4 {
			if !addr.IsUnspecified() && !addr.IsLoopback() {
				targetAddr = fmt.Sprintf("%s:%d", addr.String(), peer.ServiceInfo.Port)
				break
			}
		}

		if targetAddr == "" {
			for _, addr := range peer.ServiceInfo.AddrIPv6 {
				if !addr.IsUnspecified() && !addr.IsLoopback() {
					targetAddr = fmt.Sprintf("[%s]:%d", addr.String(), peer.ServiceInfo.Port)
					break
				}
			}
		}

		if targetAddr == "" {
			logger.Log.Warnf("无法确定对等点有效地址: %s@%s", peer.ServiceInfo.Instance, hostName)
			continue
		}

		// 发送剪贴板项目
		go func(addr string, p *Peer) {
			if pm.verboseLogging {
				logger.Log.Debugf("尝试向 %s@%s (%s) 发送剪贴板项目",
					p.ServiceInfo.Instance, hostName, addr)
			}

			// 使用TCP发送剪贴板项目
			conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err != nil {
				logger.Log.Warnf("连接到对等点 %s@%s (%s) 失败: %v",
					p.ServiceInfo.Instance, hostName, addr, err)
				return
			}
			defer conn.Close()

			// 序列化并发送
			// 这里应该实现您的序列化逻辑
			// 示例: SendClipboardItem(conn, item)
			logger.Log.Infof("成功向对等点 %s@%s 发送剪贴板项目", p.ServiceInfo.Instance, hostName)
		}(targetAddr, peer)
	}
}

// GetActivePeers 返回所有活动对等点
func (pm *PeerManager) GetActivePeers() []*Peer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var activePeers []*Peer
	for _, peer := range pm.knownPeers {
		if peer.Active {
			activePeers = append(activePeers, peer)
		}
	}
	return activePeers
}

// Stop 停止对等节点管理器
func (pm *PeerManager) Stop() {
	pm.mu.Lock()
	pm.discoveryRunning = false
	pm.mu.Unlock()
	logger.Log.Infof("PeerManager 已停止.")
}
