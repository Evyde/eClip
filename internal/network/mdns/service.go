package mdns

import (
	"context"
	"eClip/internal/logger"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

// DefaultServiceType 是默认的服务类型
const DefaultServiceType = "_eClip._tcp"

// ServiceInfo 包含服务信息
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

// RegisterService 注册mDNS服务并返回服务信息和服务监听器
func RegisterService(ctx context.Context, instanceName string, serviceType string, txtRecords []string) (*zeroconf.Server, *ServiceInfo, net.Listener, []net.Interface, error) {
	// 选择合适的网络接口
	ifaces, err := selectInterfaces()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("选择网络接口失败: %w", err)
	}

	if len(ifaces) == 0 {
		return nil, nil, nil, nil, fmt.Errorf("未找到合适的网络接口")
	}

	// 为服务创建TCP监听器
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("创建TCP监听器失败: %w", err)
	}

	// 获取分配的端口
	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		listener.Close()
		return nil, nil, nil, nil, fmt.Errorf("解析监听地址失败: %w", err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		listener.Close()
		return nil, nil, nil, nil, fmt.Errorf("解析端口号失败: %w", err)
	}

	// 格式化服务类型
	if !strings.HasPrefix(serviceType, "_") {
		serviceType = "_" + serviceType
	}
	if !strings.Contains(serviceType, "._") {
		serviceType = serviceType + "._tcp"
	}

	// 确保实例名称非空
	if instanceName == "" {
		instanceName = "eClip-Device"
	}

	// 获取主机名用于注册
	hostname, err := os.Hostname()
	if err != nil {
		listener.Close()
		return nil, nil, nil, nil, fmt.Errorf("获取主机名失败: %w", err)
	}

	// 注册mDNS服务
	var opts []zeroconf.RegisterOption
	opts = append(opts, zeroconf.SelectIfaces(ifaces))

	// 测试期间使用较短的TTL
	ttl := uint32(60)
	opts = append(opts, zeroconf.TTL(ttl))

	// 提取服务名称，移除前缀 "_"
	serviceName := serviceType
	if strings.HasPrefix(serviceName, "_") {
		serviceName = serviceName[1:]
	}
	// 分离服务名和协议
	parts := strings.SplitN(serviceName, "._", 2)
	serviceName = parts[0]

	server, err := zeroconf.Register(
		instanceName, // 实例名称
		serviceName,  // 服务类型，没有前缀 "_"
		"local.",     // 域
		port,         // 端口
		txtRecords,   // TXT记录
		opts...,      // 选项
	)

	if err != nil {
		listener.Close()
		return nil, nil, nil, nil, fmt.Errorf("注册mDNS服务失败: %w", err)
	}

	// 确保主机名有 .local 后缀
	if !strings.HasSuffix(hostname, ".local") {
		hostname = hostname + ".local"
	}

	// 创建服务信息
	info := &ServiceInfo{
		Instance: instanceName,
		Service:  serviceName,
		Domain:   "local.",
		HostName: hostname,
		Port:     port,
		TTL:      ttl,
		Text:     txtRecords,
	}

	// 获取地址信息
	addrs, err := getAllInterfaceAddrs(ifaces)
	if err != nil {
		logger.Log.Warnf("获取接口地址失败: %v", err)
	} else {
		for _, addr := range addrs {
			ip := getIPFromAddr(addr)
			if ip == nil {
				continue
			}

			if ip4 := ip.To4(); ip4 != nil {
				info.AddrIPv4 = append(info.AddrIPv4, ip4)
			} else {
				info.AddrIPv6 = append(info.AddrIPv6, ip)
			}
		}
	}

	// 等待服务注册生效
	time.Sleep(300 * time.Millisecond)

	return server, info, listener, ifaces, nil
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
