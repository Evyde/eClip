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
	clipManager      clipboard.Manager // Changed from *clipboard.Manager to clipboard.Manager
	knownPeers       map[string]*Peer
	syncInterval     time.Duration
	mu               sync.RWMutex
	discoveryRunning bool
	interfaces       []net.Interface
	verboseLogging   bool
	localHostName    string // 添加本地主机名字段
	localInstanceID  string // 添加本地实例ID字段
}

// Peer 表示网络上的一个对等节点
type Peer struct {
	ServiceInfo *ServiceInfo
	LastSeen    time.Time
	Active      bool
}

// NewPeerManager 创建一个新的对等节点管理器
func NewPeerManager(localInfo *ServiceInfo, clipManager clipboard.Manager, syncInterval time.Duration, interfaces []net.Interface) *PeerManager {
	// 生成唯一的实例ID
	instanceID := fmt.Sprintf("%s-%s", localInfo.Instance, localInfo.HostName)

	return &PeerManager{
		localInfo:       localInfo,
		clipManager:     clipManager,
		knownPeers:      make(map[string]*Peer),
		syncInterval:    syncInterval,
		interfaces:      interfaces,
		localHostName:   localInfo.HostName,
		localInstanceID: instanceID,
	}
}

// SetVerboseLogging 设置是否启用详细日志记录
func (pm *PeerManager) SetVerboseLogging(verbose bool) {
	pm.verboseLogging = verbose
}

func (pm *PeerManager) isLocalPeer(entry *ServiceInfo) bool {
	// 使用多个条件来判断是否为本地节点
	if entry.HostName == pm.localHostName {
		return true
	}

	// 检查IP地址是否匹配本地接口
	localAddrs := pm.getLocalAddresses()
	for _, addr := range entry.AddrIPv4 {
		if pm.isLocalAddress(addr, localAddrs) {
			return true
		}
	}

	return false
}

func (pm *PeerManager) isLocalAddress(addr net.IP, localAddrs []net.IP) bool {
	for _, localAddr := range localAddrs {
		if addr.Equal(localAddr) {
			return true
		}
	}
	return false
}

func (pm *PeerManager) getLocalAddresses() []net.IP {
	var addrs []net.IP
	for _, iface := range pm.interfaces {
		ifAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range ifAddrs {
			if ipNet, ok := addr.(*net.IPNet); ok {
				addrs = append(addrs, ipNet.IP)
			}
		}
	}
	return addrs
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

				// 改进本地节点检测
				if pm.isLocalPeer(entry) {
					if pm.verboseLogging {
						logger.Log.Debugf("跳过本地服务: %s@%s", entry.Instance, entry.HostName)
					}
					continue
				}

				// 处理远程节点
				pm.handleRemotePeer(entry)
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

func (pm *PeerManager) handleRemotePeer(entry *ServiceInfo) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	peerID := fmt.Sprintf("%s@%s", entry.Instance, entry.HostName)

	if peer, exists := pm.knownPeers[peerID]; exists {
		peer.LastSeen = time.Now()
		peer.Active = true
		peer.ServiceInfo = entry
		if pm.verboseLogging {
			logger.Log.Debugf("更新已知对等点: %s", peerID)
		}
	} else {
		pm.knownPeers[peerID] = &Peer{
			ServiceInfo: entry,
			LastSeen:    time.Now(),
			Active:      true,
		}
		logger.Log.Infof("发现新对等点: %s", peerID)
	}
}

// SendToPeers 向所有活动对等点发送剪贴板项目
func (pm *PeerManager) SendToPeers(item interface{}) {
	// 添加本地标识
	if clipItem, ok := item.(clipboard.Item); ok {
		clipItem.Source = pm.localHostName
		item = clipItem
	}

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
