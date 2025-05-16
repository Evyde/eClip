package mdns

import (
	"context"
	"eClip/internal/logger"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/mdns" // 新的导入路径
)

const (
	DefaultServiceType = "_eclip._tcp"
	DefaultDomain      = "local" // hashicorp/mdns 通常不包含点号
	DefaultTimeout     = 10 * time.Second
)

// ServiceInfo 保持不变，用于向 PeerManager 传递信息
type ServiceInfo struct {
	Instance string
	Service  string
	Domain   string
	HostName string
	Port     int
	AddrIPv4 []net.IP
	AddrIPv6 []net.IP
	Text     []string
}

// getSuitableInterfaces 函数不再直接用于 hashicorp/mdns 的注册或发现，
// 因为该库倾向于自动处理接口，或者通过其自身的机制指定。
// 但我们可以保留它，以防将来需要，或者用于日志记录。
// 目前，RegisterService 和 DiscoverServices 将让库自动选择接口。
func getSuitableInterfaces() ([]net.Interface, error) {
	// ... (原有实现可以保留，但当前不会被核心逻辑调用) ...
	// 为了简洁，暂时注释掉其内容，如果需要再恢复
	logger.Log.Debugf("getSuitableInterfaces: 当前未使用，hashicorp/mdns 将自动选择接口。")
	return nil, nil
}

// getActiveIPv4s 获取本机所有活动的、非环回的 IPv4 地址
func getActiveIPv4s() []net.IP {
	var activeIPs []net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		logger.Log.Errorf("getActiveIPv4s: 获取网络接口失败: %v", err)
		return nil
	}

	for _, i := range ifaces {
		if (i.Flags&net.FlagUp == 0) || (i.Flags&net.FlagLoopback != 0) {
			continue // 接口未激活或是环回接口
		}
		addrs, err := i.Addrs()
		if err != nil {
			logger.Log.Warnf("getActiveIPv4s: 无法获取接口 %s 的地址: %v", i.Name, err)
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ipv4 := ip.To4()
			if ipv4 == nil {
				continue // 不是 IPv4 地址
			}
			activeIPs = append(activeIPs, ipv4)
		}
	}
	if len(activeIPs) == 0 {
		logger.Log.Warnf("getActiveIPv4s: 未找到活动的 IPv4 地址。")
		return nil
	}
	logger.Log.Debugf("getActiveIPv4s: 找到的活动 IPv4 地址: %v", activeIPs)
	return activeIPs
}

// RegisterService 使用 hashicorp/mdns 注册服务
// 返回值调整：第一个返回值是 *mdns.Server
func RegisterService(ctx context.Context, instanceName string, serviceType string, text []string) (*mdns.Server, *ServiceInfo, net.Listener, []net.Interface, error) {
	if !strings.HasSuffix(serviceType, ".") {
		serviceType += "." //确保服务类型以点结尾，例如 "_eclip._tcp."
	}

	// 使用 os.Hostname() 作为基础主机名，并确保其格式正确
	rawHost, err := os.Hostname()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("无法获取主机名: %w", err)
	}
	mdnsHostName := rawHost
	if !strings.HasSuffix(mdnsHostName, ".local.") {
		mdnsHostName = strings.TrimSuffix(mdnsHostName, ".")
		mdnsHostName = strings.TrimSuffix(mdnsHostName, ".local")
		mdnsHostName += ".local."
	}

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("无法创建监听器: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	logger.Log.Infof("服务 %s 将在端口 %d 上注册", instanceName, port)

	activeIPv4s := getActiveIPv4s()
	if len(activeIPv4s) == 0 {
		listener.Close()
		return nil, nil, nil, nil, fmt.Errorf("未能获取任何活动的IPv4地址用于mDNS注册")
	}

	// 为服务提供明确的IPv4地址，并将host参数留空，让库使用提供的IPs
	// 或者，可以提供mdnsHostName，并同时提供activeIPv4s。
	// 根据hashicorp/mdns文档，如果HostName字段非空，则优先使用HostName。
	// 我们希望它通告我们的主机名，并使用我们提供的IPs进行解析。
	service, err := mdns.NewMDNSService(instanceName, serviceType, DefaultDomain+".", mdnsHostName, port, activeIPv4s, text)
	if err != nil {
		listener.Close()
		return nil, nil, nil, nil, fmt.Errorf("创建 mDNS 服务失败: %w", err)
	}
	// service.HostName 字段已被 NewMDNSService 设置为 mdnsHostName
	// service.IPs 字段已被 NewMDNSService 设置为 activeIPv4s
	// 我们通过仅向 NewMDNSService 提供 IPv4 地址来暗示仅 IPv4 通告。
	// 不再需要 service.AddrIPv6 = nil，因为该字段可能不存在或已废弃。

	// 创建 mDNS 服务器。传递 nil 作为接口，让库自动选择。
	// Config 中可以指定 Zone (MDNSService) 和 Iface (*net.Interface)
	// Iface: nil 表示在所有可用接口上操作
	serverConfig := &mdns.Config{Zone: service, Iface: nil}
	server, err := mdns.NewServer(serverConfig)
	if err != nil {
		listener.Close()
		return nil, nil, nil, nil, fmt.Errorf("创建 mDNS 服务器失败: %w", err)
	}

	appServiceInfo := &ServiceInfo{
		Instance: instanceName,
		Service:  serviceType,         // e.g., _eclip._tcp.
		Domain:   DefaultDomain + ".", // e.g., local.
		HostName: rawHost,             // 使用原始主机名以方便用户查看
		Port:     port,
		Text:     text,
	}

	logger.Log.Printf("mDNS 服务已注册: %s.%s%s, 主机: %s (mDNS Host: %s), 端口: %d", instanceName, serviceType, DefaultDomain+".", appServiceInfo.HostName, mdnsHostName, port)
	// 第四个返回值 []net.Interface 现在为 nil，因为库自动处理接口
	return server, appServiceInfo, listener, nil, nil
}

// DiscoverServices 使用 hashicorp/mdns 发现服务
func DiscoverServices(ctx context.Context, serviceType string, localServiceInfo *ServiceInfo, interfaces []net.Interface) ([]*ServiceInfo, error) {
	if !strings.HasSuffix(serviceType, ".") {
		serviceType += "."
	}

	entriesChan := make(chan *mdns.ServiceEntry, 10) // Buffer a bit
	discoveredServices := []*ServiceInfo{}

	discoveryCtx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	// Defer closing entriesChan to ensure the reading goroutine terminates.
	// This is important if Lookup/Query doesn't close it on context cancellation.
	defer close(entriesChan)

	go func() {
		for entry := range entriesChan {
			logger.Log.Debugf("DiscoverServices: 收到服务条目: Name:%s, Host:%s, Port:%d, AddrV4:%s, InfoFields:%v",
				entry.Name, entry.Host, entry.Port, entry.AddrV4, entry.InfoFields)

			// 从 entry.Name (e.g., "MyInstance._eclip._tcp.local.") 中提取实例名
			// entry.Service (e.g., "_eclip._tcp.local.")
			// entry.Domain (e.g., "local.")

			instanceFromName := strings.TrimSuffix(entry.Name, "."+serviceType+DefaultDomain+".")

			// 过滤自身服务
			// hashicorp/mdns 的 ServiceEntry.InfoFields 是解析后的 TXT 记录 []string{"key=value"}
			// 我们需要找到 id=UUID
			var entryUUID string
			for _, txt := range entry.InfoFields {
				if strings.HasPrefix(txt, "id=") {
					entryUUID = strings.TrimPrefix(txt, "id=")
					break
				}
			}
			localUUID := ""
			if localServiceInfo != nil && len(localServiceInfo.Text) > 2 {
				if strings.HasPrefix(localServiceInfo.Text[2], "id=") {
					localUUID = strings.TrimPrefix(localServiceInfo.Text[2], "id=")
				}
			}

			if localServiceInfo != nil && entryUUID != "" && localUUID == entryUUID && entry.Port == localServiceInfo.Port {
				logger.Log.Printf("忽略具有相同UUID (%s) 和端口 (%d) 的服务实例: %s", entryUUID, entry.Port, entry.Name)
				continue
			}

			// 过滤不同实例名的服务 (如果我们的应用逻辑需要)
			// localServiceInfo.Instance 是我们应用定义的实例名 (e.g., "Evyde")
			// instanceFromName 是从网络发现的实例名
			if localServiceInfo != nil && instanceFromName != localServiceInfo.Instance {
				logger.Log.Printf("忽略实例名不匹配的服务: 本地='%s', 发现的='%s' (来自 %s)", localServiceInfo.Instance, instanceFromName, entry.Name)
				continue
			}

			var addrsV4 []net.IP
			if entry.AddrV4 != nil {
				addrsV4 = append(addrsV4, entry.AddrV4)
			}
			var addrsV6 []net.IP
			if entry.AddrV6 != nil {
				addrsV6 = append(addrsV6, entry.AddrV6)
			}

			// 将 mdns.ServiceEntry 转换为我们自己的 ServiceInfo
			appServiceInfo := &ServiceInfo{
				Instance: instanceFromName,
				Service:  serviceType,
				Domain:   DefaultDomain + ".",
				HostName: strings.TrimSuffix(entry.Host, "."), // Remove trailing dot for consistency
				Port:     entry.Port,
				AddrIPv4: addrsV4,
				AddrIPv6: addrsV6,
				Text:     entry.InfoFields, // 使用解析后的TXT记录
			}
			discoveredServices = append(discoveredServices, appServiceInfo)
			logger.Log.Printf("处理发现的服务: %#v", appServiceInfo)
		}
		logger.Log.Printf("服务发现协程 (读取entriesChan) 结束.")
	}()

	queryParams := &mdns.QueryParam{
		Service:     serviceType,
		Domain:      DefaultDomain,
		Timeout:     DefaultTimeout, // Timeout for the mdns.Query operation
		Entries:     entriesChan,
		DisableIPv6: true, // 禁用 IPv6 进行测试
		// Interface: nil, // Let library choose interface automatically
	}

	logger.Log.Infof("DiscoverServices: 开始查询服务类型 '%s' 在域 '%s' (IPv6已禁用)，库超时 %v", queryParams.Service, queryParams.Domain, queryParams.Timeout)

	// mdns.Query will block until queryParams.Timeout, or until queryParams.Entries is closed,
	// or until queryParams.Context (if set, but it's not a field) is canceled.
	// Since QueryParam doesn't take a context for cancellation of Query itself,
	// we rely on its Timeout field. The outer discoveryCtx is for the overall DiscoverServices operation.

	err := mdns.Query(queryParams) // This is a blocking call
	if err != nil {
		// An error from Query usually means setup problems or that the query was interrupted (e.g., by closing Entries).
		// If it's a timeout, it might return a specific error or just complete.
		// The hashicorp/mdns Query func doesn't explicitly return context.DeadlineExceeded on its own timeout.
		// It simply stops sending to Entries and returns nil if its timeout is reached.
		// If Entries is closed by our defer, Query might return an error.
		logger.Log.Warnf("mdns.Query 完成，可能伴有错误/超时: %v", err)
	} else {
		logger.Log.Debugf("mdns.Query 成功完成 (可能因超时).")
	}

	// discoveryCtx ensures that DiscoverServices itself times out.
	// The reading goroutine will terminate when entriesChan is closed by the defer statement
	// after discoveryCtx is done.
	<-discoveryCtx.Done()
	logger.Log.Debugf("DiscoverServices: discoveryCtx 完成 (%v超时), 确保所有处理结束.", DefaultTimeout)

	return discoveredServices, nil
}
