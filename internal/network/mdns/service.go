package mdns

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	// DefaultServiceType 是 mDNS 服务的默认类型
	DefaultServiceType = "_eclip._tcp"
	// DefaultDomain 是 mDNS 服务的默认域
	DefaultDomain = "local."
	// DefaultTimeout 是 mDNS 操作的默认超时时间
	DefaultTimeout = 5 * time.Second
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

// RegisterService 注册一个新的 mDNS 服务，并使用动态端口
// 它返回 zeroconf 服务器、服务信息、创建的监听器和错误
func RegisterService(ctx context.Context, instanceName string, serviceType string, text []string) (*zeroconf.Server, *ServiceInfo, net.Listener, error) {
	if instanceName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("无法获取主机名: %w", err)
		}
		instanceName = hostname
	}
	if serviceType == "" {
		serviceType = DefaultServiceType
	}

	// 创建一个监听器以获取动态端口
	listener, err := net.Listen("tcp", ":0") // ":0" 表示动态分配端口
	if err != nil {
		return nil, nil, nil, fmt.Errorf("无法创建监听器以获取动态端口: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	// 不要在这里关闭监听器；它将被返回并由调用者（或 ClipboardServer）使用
	// defer listener.Close() // 调用者负责关闭监听器

	log.Printf("服务 %s 将在端口 %d 上注册（使用提供的监听器）\n", instanceName, port)

	server, err := zeroconf.Register(
		instanceName,  // 服务实例名, e.g., "My eClip Instance"
		serviceType,   // 服务类型, e.g., "_eclip._tcp"
		DefaultDomain, // 域名, e.g., "local."
		port,          // 服务端口
		text,          // 服务的附加 TXT 记录, e.g., []string{"version=1.0"}
		nil,           // 网络接口，nil 表示所有接口
	)
	if err != nil {
		listener.Close() // 如果注册失败，关闭监听器
		return nil, nil, nil, fmt.Errorf("无法注册 mDNS 服务: %w", err)
	}

	serviceInfo := &ServiceInfo{
		Instance: instanceName,
		Service:  serviceType,
		Domain:   DefaultDomain,
		HostName: instanceName, // 通常实例名可以作为主机名，或者从 os.Hostname() 获取更精确的
		Port:     port,
		Text:     text,
	}

	log.Printf("mDNS 服务已注册: %s.%s%s, 主机: %s, 端口: %d\n", instanceName, serviceType, DefaultDomain, serviceInfo.HostName, port)
	return server, serviceInfo, listener, nil
}

// DiscoverServices 发现指定类型的 mDNS 服务，并排除本地服务实例
func DiscoverServices(ctx context.Context, serviceType string, localServiceInfo *ServiceInfo) ([]*ServiceInfo, error) {
	if serviceType == "" {
		serviceType = DefaultServiceType
	}

	resolver, err := zeroconf.NewResolver(nil) // nil 表示使用默认网络接口
	if err != nil {
		return nil, fmt.Errorf("无法创建 mDNS 解析器: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	discoveredServices := []*ServiceInfo{}

	discoveryCtx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	go func(results <-chan *zeroconf.ServiceEntry) {
		for entry := range results {
			// 检查是否是本地服务实例
			// 我们需要确保 localServiceInfo 不为 nil，并且其 Instance 和 Port 已被正确设置
			// 通过比较实例名和端口来确保只排除完全相同的服务实例
			if localServiceInfo != nil && entry.Instance == localServiceInfo.Instance && entry.Port == localServiceInfo.Port {
				log.Printf("忽略具有相同实例名和端口的本地服务实例: %s (%s:%d)\n", entry.Instance, entry.HostName, entry.Port)
				continue // 跳过完全相同的本地实例
			}

			log.Printf("发现服务: 实例: %s, 服务: %s, 域: %s, 主机: %s, 端口: %d, IPv4: %v, IPv6: %v, TXT: %v\n",
				entry.Instance, entry.Service, entry.Domain, entry.HostName, entry.Port, entry.AddrIPv4, entry.AddrIPv6, entry.Text)
			discoveredServices = append(discoveredServices, &ServiceInfo{
				Instance: entry.Instance,
				Service:  entry.Service,
				Domain:   entry.Domain,
				HostName: entry.HostName,
				Port:     entry.Port,
				AddrIPv4: entry.AddrIPv4,
				AddrIPv6: entry.AddrIPv6,
				Text:     entry.Text,
			})
		}
		log.Println("服务发现协程结束.")
	}(entries)

	log.Printf("开始浏览服务类型: %s.%s\n", serviceType, DefaultDomain)
	err = resolver.Browse(discoveryCtx, serviceType, DefaultDomain, entries)
	if err != nil {
		return nil, fmt.Errorf("浏览 mDNS 服务失败: %w", err)
	}

	<-discoveryCtx.Done() // 等待浏览超时或上下文取消
	log.Printf("服务浏览完成. 发现了 %d 个服务.\n", len(discoveredServices))

	return discoveredServices, nil
}

// Example usage (can be moved to a main or test file)
/*
func main() {
	ctx := context.Background()

	// 注册服务
	go func() {
		// 模拟服务端的 listener，实际应用中服务逻辑会使用它
		tempListener, err := net.Listen("tcp", ":0")
		if err != nil {
			log.Fatalf("无法创建临时监听器: %v", err)
		}
		dynamicPort := tempListener.Addr().(*net.TCPAddr).Port
		tempListener.Close() // 立即关闭，因为我们只用端口来演示注册

		// 重新实现 RegisterService 以便传递端口而不是动态获取
		// 或者修改 RegisterService 以接受一个 listener
		// 这里为了简单，我们假设 RegisterService 内部处理了端口

		// 假设我们有一个服务实例名
		instanceName := "MyEclipInstance"
		// 假设我们有一些元数据
		txtRecords := []string{"version=0.1", "user=testuser"}

		// 包装 RegisterService 调用以匹配其签名
		registerFunc := func(port int) (*zeroconf.Server, *ServiceInfo, error) {
			server, err := zeroconf.Register(
				instanceName,
				DefaultServiceType,
				DefaultDomain,
				port,
				txtRecords,
				nil,
			)
			if err != nil {
				return nil, nil, err
			}
			serviceInfo := &ServiceInfo{
				Instance: instanceName,
				Service:  DefaultServiceType,
				Domain:   DefaultDomain,
				HostName: instanceName,
				Port:     port,
				Text:     txtRecords,
			}
			return server, serviceInfo, nil
		}


		mdnsServer, serviceInfo, err := registerFunc(dynamicPort)
		// mdnsServer, serviceInfo, err := RegisterService(ctx, "MyEclipInstance", DefaultServiceType, []string{"version=0.1"})
		if err != nil {
			log.Fatalf("无法注册服务: %v", err)
		}
		log.Printf("服务注册成功: %+v\n", serviceInfo)
		defer mdnsServer.Shutdown()

		// 保持服务运行
		select {}
	}()

	// 等待一段时间让服务注册
	time.Sleep(2 * time.Second)

	// 发现服务
	log.Println("开始发现服务...")
	services, err := DiscoverServices(ctx, DefaultServiceType)
	if err != nil {
		log.Fatalf("无法发现服务: %v", err)
	}

	if len(services) == 0 {
		log.Println("未发现任何服务.")
	} else {
		log.Printf("发现的服务列表 (%d):\n", len(services))
		for i, s := range services {
			log.Printf("  %d: %+v\n", i+1, s)
		}
	}
}
*/
