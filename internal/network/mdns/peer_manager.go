package mdns

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os" // 新增导入
	"sync"
	"time"

	"eClip/internal/core/clipboard"
	"eClip/internal/logger"  // 假设有一个日志包
	"eClip/internal/network" // Added for network.ClipData
)

const (
	// syncServerPortOffset = 1000 // No longer needed as PeerManager doesn't run its own server
	defaultDeviceID = "unknown_device" // 默认设备ID
	connectTimeout  = 5 * time.Second
	// readDeadline         = 10 * time.Second // No longer needed here, ClipboardServer handles its own read deadlines
)

// Peer 代表一个网络中的对等节点
type Peer struct {
	ID          string // 通常是 ServiceInfo.Instance
	ServiceInfo *ServiceInfo
	// conn        net.Conn // 与该对等点的网络连接, 暂时不在 Peer 结构中管理，而是在发送时建立
}

// PeerManager 管理已发现的对等节点
type PeerManager struct {
	mu           sync.RWMutex
	peers        map[string]*Peer
	localSvcInfo *ServiceInfo  // 本地服务的信息，用于避免连接自身和设置源ID
	syncInterval time.Duration // 服务发现间隔
	stopChan     chan struct{} // 用于停止服务发现
	// clipboardManager clipboard.Manager // No longer needed as PeerManager doesn't handle incoming data
	// isServerRunning  bool              // No longer needed
	// listener         net.Listener      // No longer needed
}

// NewPeerManager 创建一个新的 PeerManager
// clipManager is removed as PeerManager no longer handles incoming data directly.
func NewPeerManager(localServiceInfo *ServiceInfo, _ clipboard.Manager, syncInterval time.Duration) *PeerManager {
	return &PeerManager{
		peers:        make(map[string]*Peer),
		localSvcInfo: localServiceInfo,
		// clipboardManager: clipManager, // Removed
		syncInterval: syncInterval,
		stopChan:     make(chan struct{}),
	}
}

// AddPeer 添加或更新一个对等节点
func (pm *PeerManager) AddPeer(svcInfo *ServiceInfo) {
	// 仅当实例名和端口都相同时，才忽略添加本地服务
	if pm.localSvcInfo != nil && svcInfo.Text[2] == pm.localSvcInfo.Text[2] && svcInfo.Port == pm.localSvcInfo.Port {
		logger.Log.Debugf("忽略具有相同UUID和端口的服务实例: %s (%s:%d)\n", svcInfo.Text[2], svcInfo.HostName, svcInfo.Port)
		return // 跳过完全相同的本地实例
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if peer, exists := pm.peers[svcInfo.Instance]; exists {
		logger.Log.Debugf("PeerManager: 更新对等节点 %s", svcInfo.Instance)
		peer.ServiceInfo = svcInfo
	} else {
		logger.Log.Infof("PeerManager: 添加新的对等节点 %s (%s:%d)", svcInfo.Instance, svcInfo.HostName, svcInfo.Port)
		pm.peers[svcInfo.Instance] = &Peer{
			ID:          svcInfo.Instance,
			ServiceInfo: svcInfo,
		}
	}
}

// RemovePeer 移除一个对等节点
func (pm *PeerManager) RemovePeer(instanceID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if _, exists := pm.peers[instanceID]; exists {
		delete(pm.peers, instanceID)
		logger.Log.Infof("PeerManager: 移除对等节点 %s", instanceID)
	}
}

// GetPeers 返回所有对等节点的列表
func (pm *PeerManager) GetPeers() []*Peer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	peersList := make([]*Peer, 0, len(pm.peers))
	for _, peer := range pm.peers {
		peersList = append(peersList, peer)
	}
	return peersList
}

// StartDiscovery 启动 mDNS 服务发现并定期更新对等节点列表
func (pm *PeerManager) StartDiscovery(ctx context.Context, serviceType string) {
	if serviceType == "" {
		serviceType = DefaultServiceType
	}

	logger.Log.Infof("PeerManager: 开始服务发现，类型: %s, 间隔: %v", serviceType, pm.syncInterval)

	// 确保 stopChan 在多次调用 StartDiscovery 时不会重复创建
	// 但通常 StartDiscovery 和 StartSyncServer 应该只被调用一次

	ticker := time.NewTicker(pm.syncInterval)
	// defer ticker.Stop() // ticker.Stop() 会在 for select 循环退出时执行

	// 立即执行一次发现
	pm.discoverAndAddPeers(ctx, serviceType)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				pm.discoverAndAddPeers(ctx, serviceType)
			case <-pm.stopChan:
				logger.Log.Infof("PeerManager: 停止服务发现（来自 stopChan）")
				return
			case <-ctx.Done():
				logger.Log.Infof("PeerManager: 服务发现上下文取消")
				pm.Stop() // 确保所有资源都已清理
				return
			}
		}
	}()
}

func (pm *PeerManager) discoverAndAddPeers(ctx context.Context, serviceType string) {
	logger.Log.Debugf("PeerManager: 正在发现服务类型 %s ...", serviceType)
	// 将 pm.localSvcInfo 传递给 DiscoverServices
	discoveredServices, err := DiscoverServices(ctx, serviceType, pm.localSvcInfo)
	if err != nil {
		logger.Log.Errorf("PeerManager: 服务发现失败: %v", err)
		return
	}

	logger.Log.Debugf("PeerManager: 发现了 %d 个服务", len(discoveredServices))
	// currentPeersOnNetwork := make(map[string]bool) // 用于检测已消失的对等点

	for _, svcInfo := range discoveredServices {
		pm.AddPeer(svcInfo)
		// currentPeersOnNetwork[svcInfo.Instance] = true
	}

	// 可选: 清理不再存在的对等节点
	// pm.mu.Lock()
	// for id := range pm.peers {
	// 	if !currentPeersOnNetwork[id] {
	// 		logger.Log.Infof("PeerManager: 对等节点 %s 不再可见，正在移除", id)
	// 		delete(pm.peers, id)
	// 	}
	// }
	// pm.mu.Unlock()
}

// Stop 停止服务发现和同步服务器
func (pm *PeerManager) Stop() {
	logger.Log.Infof("PeerManager: 正在停止...")
	// 使用 select 防止重复关闭 stopChan
	select {
	case <-pm.stopChan:
		// 已经关闭或正在关闭
	default:
		close(pm.stopChan)
	}
	// No listener to close here anymore
	logger.Log.Infof("PeerManager: 服务发现已停止。")
}

// SendToPeers 将剪切板项目发送给所有对等节点
func (pm *PeerManager) SendToPeers(item clipboard.ClipItem) {
	pm.mu.RLock()
	peersSnapshot := make([]*Peer, 0, len(pm.peers))
	for _, p := range pm.peers {
		peersSnapshot = append(peersSnapshot, p)
	}
	pm.mu.RUnlock()

	if item.Source == "" {
		if pm.localSvcInfo != nil && pm.localSvcInfo.Instance != "" {
			item.Source = pm.localSvcInfo.Instance
		} else {
			hostname, err := os.Hostname()
			if err == nil && hostname != "" {
				item.Source = hostname
			} else {
				item.Source = defaultDeviceID
			}
			logger.Log.Warnf("PeerManager: ClipItem.Source 为空，已设置为 %s", item.Source)
		}
	}

	// Convert clipboard.ClipItem to network.ClipData
	clipData := network.ClipData{
		Type:    item.Type,
		Content: item.Content, // Assuming item.Content is already in the correct []byte form
		Source:  item.Source,
	}

	// Special handling for File type: ClipItem.Content might be []string for files,
	// but network.ClipData.Content expects []byte.
	// If item.Type is File, item.Content should be marshaled if it's not already raw bytes.
	// However, clipboard.ClipItem.Content is already []byte.
	// If it was originally a list of file paths, it should have been marshaled into item.Content by the clipboard manager.
	// For now, we assume item.Content is directly usable.
	// If item.Type == clipboard.File, and item.Content was meant to be a list of paths,
	// the sender (clipboard monitor) or clipboard.Manager.GetCurrentContent should prepare item.Content as marshaled JSON of []string.
	// The network.ClipData.Content will then carry this marshaled JSON byte array.
	// The receiver (ClipboardServer) will see clipboard.File type and unmarshal Content back to []string.
	// This part needs careful review of how clipboard.File is handled end-to-end.
	// For now, direct assignment is used.

	jsonData, err := json.Marshal(clipData)
	if err != nil {
		logger.Log.Errorf("PeerManager: 序列化 network.ClipData 失败: %v", err)
		return
	}

	logger.Log.Debugf("PeerManager: 准备将 network.ClipData (源: %s, 类型: %d) 发送给 %d 个对等节点", clipData.Source, clipData.Type, len(peersSnapshot))
	for _, peer := range peersSnapshot {
		go pm.sendDataToPeer(peer, jsonData)
	}
}

func (pm *PeerManager) sendDataToPeer(peer *Peer, data []byte) {
	if peer.ServiceInfo == nil {
		logger.Log.Warnf("PeerManager: 对等节点 %s 没有 ServiceInfo", peer.ID)
		return
	}

	// 目标端口是 mDNS 服务发布的端口，因为 ClipboardServer 在那里监听
	targetPort := peer.ServiceInfo.Port
	if targetPort == 0 {
		logger.Log.Warnf("PeerManager: 对等节点 %s 的服务端口为0，无法连接", peer.ID)
		return
	}

	var targetAddr string
	// 优先使用 IP 地址 (IPv4 > IPv6)，然后回退到 HostName
	if len(peer.ServiceInfo.AddrIPv4) > 0 {
		targetAddr = fmt.Sprintf("%s:%d", peer.ServiceInfo.AddrIPv4[0].String(), targetPort)
		logger.Log.Debugf("PeerManager: 使用 IPv4 地址 %s 连接对等节点 %s", targetAddr, peer.ID)
	} else if len(peer.ServiceInfo.AddrIPv6) > 0 {
		// IPv6 地址需要用方括号括起来
		targetAddr = fmt.Sprintf("[%s]:%d", peer.ServiceInfo.AddrIPv6[0].String(), targetPort)
		logger.Log.Debugf("PeerManager: 使用 IPv6 地址 %s 连接对等节点 %s", targetAddr, peer.ID)
	} else if peer.ServiceInfo.HostName != "" {
		targetAddr = fmt.Sprintf("%s:%d", peer.ServiceInfo.HostName, targetPort)
		logger.Log.Debugf("PeerManager: 使用主机名 %s 连接对等节点 %s", targetAddr, peer.ID)
	} else {
		logger.Log.Warnf("PeerManager: 对等节点 %s (%s) 没有有效的 IP 地址或主机名", peer.ID, peer.ServiceInfo.Instance)
		return
	}

	logger.Log.Debugf("PeerManager: 尝试连接到对等节点 %s (%s) 发送数据", peer.ID, targetAddr)
	conn, err := net.DialTimeout("tcp", targetAddr, connectTimeout)
	if err != nil {
		// 如果使用 IP 地址连接失败，并且有主机名，可以尝试回退到主机名
		// 但如果最初就因为主机名解析失败，这里的回退可能意义不大，除非网络环境变化
		// 为简单起见，我们暂时不添加复杂的回退逻辑，优先IP的策略应该能改善大部分情况
		logger.Log.Errorf("PeerManager: 连接到对等节点 %s (%s) 失败: %v", peer.ID, targetAddr, err)
		// pm.RemovePeer(peer.ID) // 可选：连接失败则移除
		return
	}
	defer conn.Close()

	// data is already marshaled JSON bytes of network.ClipData
	_, err = conn.Write(data)
	if err != nil {
		logger.Log.Errorf("PeerManager: 发送数据到对等节点 %s 失败: %v", peer.ID, err)
		return
	}
	logger.Log.Infof("PeerManager: 成功发送数据到对等节点 %s", peer.ID)
}

// StartSyncServer and handleIncomingConnection are removed as this functionality
// is now handled by network.ClipboardServer, started in main.go.
// The PeerManager is now only responsible for discovering peers and sending data to them.
