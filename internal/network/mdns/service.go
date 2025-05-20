package mdns

import (
	"context"
	"eClip/internal/logger"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	// DefaultServiceType 是 mDNS 服务的默认类型
	DefaultServiceType = "_eclip._tcp"
	// DefaultDomain 是 mDNS 服务的默认域
	DefaultDomain = "local."
	// DefaultTimeout 是 mDNS 操作的默认超时时间
	DefaultTimeout = 10 * time.Second // 设置为 10 秒
)

// ServiceInfo 存储了 mDNS 服务的信息
type ServiceInfo struct {
	Instance string
	Service  string
	Domain   string
	HostName string
	Port     int
	TTL      uint32
	Text     []string
	AddrIPv4 []net.IP
	AddrIPv6 []net.IP
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

	// 使用新的 zeroconf.Browse API
	entries := make(chan *zeroconf.ServiceEntry)
	discoveredServices := []*ServiceInfo{}

	discoveryCtx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	// 准备 ClientOption，如果需要传递特定接口的话
	// 由于 RegisterService 现在返回 nil 接口，并且我们倾向于让库自动选择，
	// 这里的 interfaces 参数也将是 nil。
	// 因此，我们将不传递 opts 给 Browse，让其使用默认接口。
	var browseOpts []zeroconf.ClientOption
	if len(interfaces) > 0 {
		// 这个分支实际上不会被走到，因为 interfaces 总是 nil
		logger.Log.Debugf("DiscoverServices: 准备使用特定接口进行浏览: %v", interfaces)
		browseOpts = append(browseOpts, zeroconf.SelectIfaces(interfaces))
	} else {
		logger.Log.Infof("DiscoverServices: 将让 zeroconf 库自动选择发现接口。")
	}

	go func(results <-chan *zeroconf.ServiceEntry) {
		for entry := range results {
			logger.Log.Debugf("DiscoverServices: 收到服务条目: %s", entry)

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

	logger.Log.Infof("DiscoverServices: 开始浏览服务类型: %s.%s", serviceType, DefaultDomain)
	// 直接调用包级别的 Browse 函数
	err := zeroconf.Browse(discoveryCtx, serviceType, DefaultDomain, entries, browseOpts...)
	if err != nil {
		// 注意：如果 Browse 因为 ctx.Done() 而返回，它可能会返回一个错误，例如 context.Canceled。
		// 我们需要区分是真正的浏览启动失败，还是因为超时/取消而正常结束。
		// 然而，zeroconf.Browse 的文档说它 "blocks until the context is canceled (or an error occurs)"
		// 所以这里的错误通常是启动错误。
		return nil, fmt.Errorf("浏览 mDNS 服务失败: %w", err)
	}
	// Browse 函数会阻塞直到 discoveryCtx 被取消 (例如超时)
	// 或者发生其他错误导致它返回。
	// 当 Browse 返回时，通常意味着发现周期结束或出错。
	// 我们已经在 goroutine 中处理 entries，并在 discoveryCtx.Done() 后返回。
	// 这里的 <-discoveryCtx.Done() 是为了确保函数在超时或取消后才返回。

	<-discoveryCtx.Done()
	logger.Log.Debugf("DiscoverServices: discoveryCtx 完成.")

	return discoveredServices, nil
}

// 选择合适的网络接口
func selectInterfaces() ([]net.Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var filtered []net.Interface
	for _, iface := range ifaces {
		// 忽略禁用的接口
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		// 忽略回环接口
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// 检查接口是否有可用地址
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		hasValidAddr := false
		for _, addr := range addrs {
			ip := getIPFromAddr(addr)
			if ip == nil {
				continue
			}

			// 跳过链路本地地址
			if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				continue
			}

			// 有效地址
			hasValidAddr = true
			break
		}

		if hasValidAddr {
			filtered = append(filtered, iface)
		}
	}

	return filtered, nil
}

// 获取网络接口的所有地址
func getAllInterfaceAddrs(ifaces []net.Interface) ([]net.Addr, error) {
	var allAddrs []net.Addr
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		allAddrs = append(allAddrs, addrs...)
	}
	return allAddrs, nil
}

// 从网络地址获取IP
func getIPFromAddr(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	}
	return nil
}

// 获取本地主机名
func getLocalHostname(server *zeroconf.Server) (string, error) {
	hostname, err := net.LookupCNAME(server.TTL())
	if err != nil {
		hostname, err = os.Hostname()
		if err != nil {
			return "", err
		}
	}

	// 移除尾部的点
	hostname = strings.TrimSuffix(hostname, ".")

	// 确保有.local后缀
	if !strings.HasSuffix(hostname, ".local") {
		hostname = hostname + ".local"
	}

	return hostname, nil
}
