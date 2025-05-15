package mdns

import (
	"context"
	"eClip/internal/logger"
	"fmt"
	"net"
	"os"      // 新增导入，用于潜在的 OS 特定逻辑
	"strings" // 新增导入
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	// DefaultServiceType 是 mDNS 服务的默认类型
	DefaultServiceType = "_eclip._tcp"
	// DefaultDomain 是 mDNS 服务的默认域
	DefaultDomain = "local."
	// DefaultTimeout 是 mDNS 操作的默认超时时间
	DefaultTimeout = 1 * time.Second // 缩短到 1 秒
)

// ServiceInfo 存储了 mDNS 服务的信息
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

// getSuitableInterfaces 获取用于 mDNS 注册的合适网络接口。
// 它会选择 UP、支持 MULTICAST 且非 LOOPBACK 的接口。
func getSuitableInterfaces() ([]net.Interface, error) {
	var suitableInterfaces []net.Interface
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("获取网络接口失败: %w", err)
	}

	for _, ifi := range interfaces {
		if (ifi.Flags&net.FlagUp == 0) || (ifi.Flags&net.FlagMulticast == 0) || (ifi.Flags&net.FlagLoopback != 0) {
			logger.Log.Debugf("跳过不合适的接口: %s, Flags: %s", ifi.Name, ifi.Flags.String())
			continue
		}

		// 附加检查：确保接口至少有一个可用的 IP 地址（可选，但推荐）
		addrs, err := ifi.Addrs()
		if err != nil {
			logger.Log.Warnf("无法获取接口 %s 的地址，跳过: %v", ifi.Name, err)
			continue
		}
		if len(addrs) == 0 {
			logger.Log.Debugf("接口 %s 没有地址，跳过", ifi.Name)
			continue
		}

		// 检查是否有有效的 IPv4 或 IPv6 地址 (非仅链接本地)
		hasValidIP := false
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && !ipnet.IP.IsLinkLocalUnicast() {
				if ipnet.IP.To4() != nil || ipnet.IP.To16() != nil {
					hasValidIP = true
					break
				}
			}
		}
		if !hasValidIP {
			logger.Log.Debugf("接口 %s 没有有效的全局单播 IPv4/IPv6 地址，跳过", ifi.Name)
			continue
		}

		logger.Log.Debugf("找到合适的接口: %s, Flags: %s, MTU: %d", ifi.Name, ifi.Flags.String(), ifi.MTU)
		suitableInterfaces = append(suitableInterfaces, ifi)
	}

	if len(suitableInterfaces) == 0 {
		// 如果没有找到合适的接口，返回 nil，让 zeroconf 库决定使用哪些接口（通常是所有接口）
		// 这在某些虚拟化环境中可能是必要的，或者如果上述过滤过于严格
		logger.Log.Warnf("未找到符合特定条件的网络接口。将依赖 zeroconf 库的默认接口选择。")
		return nil, nil
	}

	selectedInterfaceNames := make([]string, len(suitableInterfaces))
	for i, ifi := range suitableInterfaces {
		selectedInterfaceNames[i] = ifi.Name
	}
	logger.Log.Infof("为 mDNS 选择的特定网络接口: %v", selectedInterfaceNames)
	return suitableInterfaces, nil
}

// RegisterService 注册一个新的 mDNS 服务，并使用动态端口
// 它返回 zeroconf 服务器、服务信息、创建的监听器、选择的网络接口和错误
func RegisterService(ctx context.Context, instanceName string, serviceType string, text []string) (*zeroconf.Server, *ServiceInfo, net.Listener, []net.Interface, error) {
	if serviceType == "" {
		serviceType = DefaultServiceType
	}

	hostname, err := os.Hostname()

	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("无法获取主机名: %w", err)
	}
	sanitizedHostName := strings.TrimSuffix(hostname, DefaultDomain)
	sanitizedHostName = strings.TrimSuffix(sanitizedHostName, ".local")

	// 创建一个监听器以获取动态端口
	listener, err := net.Listen("tcp", ":0") // ":0" 表示动态分配端口
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("无法创建监听器以获取动态端口: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	// 不要在这里关闭监听器；它将被返回并由调用者（或 ClipboardServer）使用
	// defer listener.Close() // 调用者负责关闭监听器

	logger.Log.Infof("服务 %s 将在端口 %d 上注册（使用提供的监听器）", instanceName, port)

	// 让库自动选择网络接口进行注册
	logger.Log.Infof("RegisterService: 将让 zeroconf 库自动选择注册接口。")
	server, err := zeroconf.Register(
		instanceName,  // 服务实例名, e.g., "My eClip Instance"
		serviceType,   // 服务类型, e.g., "_eclip._tcp"
		DefaultDomain, // 域名, e.g., "local."
		port,          // 服务端口
		text,          // 服务的附加 TXT 记录, e.g., []string{"version=1.0"}
		nil,           // 总是传递 nil，让库自动选择接口
	)
	if err != nil {
		listener.Close() // 如果注册失败，关闭监听器
		return nil, nil, nil, nil, fmt.Errorf("无法注册 mDNS 服务: %w", err)
	}

	serviceInfo := &ServiceInfo{
		Instance: instanceName,
		Service:  serviceType,
		Domain:   DefaultDomain,
		HostName: sanitizedHostName, // 通常实例名可以作为主机名，或者从 os.Hostname() 获取更精确的
		Port:     port,
		Text:     text,
	}

	logger.Log.Printf("mDNS 服务已注册: %s.%s %s, 主机: %s, 端口: %d", instanceName, serviceType, DefaultDomain, serviceInfo.HostName, port)
	// 由于我们不再选择接口，返回 nil 作为接口列表
	return server, serviceInfo, listener, nil, nil
}

// DiscoverServices 发现指定类型的 mDNS 服务，并排除本地服务实例
// interfaces 参数指定用于发现的网络接口，如果为 nil，则使用默认接口
func DiscoverServices(ctx context.Context, serviceType string, localServiceInfo *ServiceInfo, interfaces []net.Interface) ([]*ServiceInfo, error) {
	if serviceType == "" {
		serviceType = DefaultServiceType
	}

	// 使用提供的接口或默认接口创建解析器
	var resolver *zeroconf.Resolver
	var err error

	// 总是让库自动选择网络接口进行发现
	// interfaces 参数（来自 PeerManager，最终来自 RegisterService 的返回）现在总是 nil，但我们在这里明确忽略它
	logger.Log.Infof("DiscoverServices: 将让 zeroconf 库自动选择发现接口 (传递的interfaces参数被忽略: %v)", interfaces)
	resolver, err = zeroconf.NewResolver() // 总是使用默认接口

	if err != nil {
		return nil, fmt.Errorf("无法创建 mDNS 解析器: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	discoveredServices := []*ServiceInfo{}

	discoveryCtx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	go func(results <-chan *zeroconf.ServiceEntry) {
		for entry := range results {
			logger.Log.Debugf("发现服务: %s", entry)

			// 检查是否是本地服务实例
			// 我们需要确保 localServiceInfo 不为 nil，并且其 Instance 和 Port 已被正确设置
			// 通过比较实例名和端口来确保只排除完全相同的服务实例
			if localServiceInfo != nil && entry.Text[2] == localServiceInfo.Text[2] && entry.Port == localServiceInfo.Port {
				logger.Log.Printf("忽略具有相同UUID和端口的服务实例: %s (%s:%d)", entry.Text[2], entry.HostName, entry.Port)
				continue // 跳过完全相同的本地实例
			}

			if localServiceInfo != nil && entry.Instance != localServiceInfo.Instance {
				logger.Log.Printf("忽略别人的服务实例: [%s]-x-[%s]", entry.Instance, localServiceInfo.Instance)
				continue // 跳过完全相同的本地实例
			}

			sanitizedHostName := entry.HostName
			// 清理主机名：如果 entry.HostName 以 entry.Domain 结尾，
			// 并且移除该后缀后的字符串仍然以 entry.Domain 结尾，
			// 说明原始主机名包含了重复的域名，例如 "host.local.local."。
			// 这种情况下，我们使用移除一次后缀后的结果 "host.local."。
			if entry.Domain != "" && strings.HasSuffix(entry.HostName, entry.Domain) {
				tempHostName := strings.TrimSuffix(entry.HostName, entry.Domain)
				// 确保 tempHostName 不是空字符串，并且在移除第一个域名后，剩余部分仍然以域名结尾
				// 例如：HostName="host.local.local.", Domain="local." -> tempHostName="host.local."
				//       strings.HasSuffix("host.local.", "local.") is true.
				// 例如：HostName="host.local.", Domain="local." -> tempHostName="host."
				//       strings.HasSuffix("host.", "local.") is false.
				if tempHostName != "" && strings.HasSuffix(tempHostName, entry.Domain) {
					sanitizedHostName = tempHostName
				}
			}

			logger.Log.Printf("发现服务: 实例: %s, 服务: %s, 域: %s, 主机: %s (原始: %s), 端口: %d, IPv4: %v, IPv6: %v, TXT: %v\n",
				entry.Instance, entry.Service, entry.Domain, sanitizedHostName, entry.HostName, entry.Port, entry.AddrIPv4, entry.AddrIPv6, entry.Text)
			discoveredServices = append(discoveredServices, &ServiceInfo{
				Instance: entry.Instance,
				Service:  entry.Service,
				Domain:   entry.Domain,
				HostName: sanitizedHostName, // 使用清理后的主机名
				Port:     entry.Port,
				AddrIPv4: entry.AddrIPv4,
				AddrIPv6: entry.AddrIPv6,
				Text:     entry.Text,
			})
		}
		logger.Log.Printf("服务发现协程结束.")
	}(entries)

	logger.Log.Printf("开始浏览服务类型: %s.%s", serviceType, DefaultDomain)
	err = resolver.Browse(discoveryCtx, serviceType, DefaultDomain, entries)
	if err != nil {
		return nil, fmt.Errorf("浏览 mDNS 服务失败: %w", err)
	}

	<-discoveryCtx.Done() // 等待浏览超时或上下文取消

	return discoveredServices, nil
}
