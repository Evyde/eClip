package mdns

import (
	"context"
	"eClip/internal/logger"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/grandcat/zeroconf"
)

// Browser 服务发现浏览器
type Browser struct {
	serviceType string
	resolver    *zeroconf.Resolver
	interfaces  []net.Interface
	closed      bool
	mu          sync.Mutex
}

// NewBrowser 创建一个新的mDNS服务浏览器
func NewBrowser(serviceType string, interfaces []net.Interface) (*Browser, error) {
	// 确保服务类型格式正确
	if !strings.HasSuffix(serviceType, ".") {
		serviceType += "."
	}
	if !strings.HasSuffix(serviceType, "local.") {
		serviceType += "local."
	}

	// 创建zeroconf解析器，特别指定使用的网络接口
	var opts []zeroconf.ClientOption
	if len(interfaces) > 0 {
		opts = append(opts, zeroconf.SelectIfaces(interfaces))
	}

	// 禁用IPv6可能有助于解决跨平台兼容性问题
	// opts = append(opts, zeroconf.WithIPProtocol(zeroconf.IPv4Only))

	resolver, err := zeroconf.NewResolver(opts...)
	if err != nil {
		return nil, fmt.Errorf("创建mDNS解析器失败: %w", err)
	}

	return &Browser{
		serviceType: serviceType,
		resolver:    resolver,
		interfaces:  interfaces,
	}, nil
}

// Browse 开始浏览服务
func (b *Browser) Browse(ctx context.Context) <-chan *ServiceInfo {
	entries := make(chan *ServiceInfo, 10)

	go func() {
		defer close(entries)

		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()

		// 解析服务类型名称
		serviceName, serviceSubtype := parseServiceType(b.serviceType)
		logger.Log.Debugf("开始浏览mDNS服务 %s (子类型: %s)", serviceName, serviceSubtype)

		// 创建结果通道
		results := make(chan *zeroconf.ServiceEntry, 10)

		// 启动浏览
		err := b.resolver.Browse(ctx, serviceName, serviceSubtype, results)
		if err != nil {
			logger.Log.Errorf("启动mDNS浏览失败: %v", err)
			return
		}

		// 转换结果
		for entry := range results {
			service := &ServiceInfo{
				Instance: entry.Instance,
				Service:  entry.Service,
				Domain:   entry.Domain,
				HostName: entry.HostName,
				Port:     entry.Port,
				TTL:      entry.TTL,
				Text:     entry.Text,
				AddrIPv4: entry.AddrIPv4,
				AddrIPv6: entry.AddrIPv6,
			}

			logger.Log.Debugf("发现服务: %s@%s (IPv4: %v, IPv6: %v, Port: %d)",
				service.Instance, service.HostName, service.AddrIPv4, service.AddrIPv6, service.Port)

			select {
			case <-ctx.Done():
				return
			case entries <- service:
			}
		}
	}()

	return entries
}

// Close 关闭浏览器
func (b *Browser) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
}

// 解析服务类型名称
func parseServiceType(serviceType string) (serviceName, serviceSubtype string) {
	// 默认没有子类型
	serviceSubtype = ""

	// 移除可能的"._"前缀
	if strings.HasPrefix(serviceType, "._") {
		serviceType = serviceType[1:]
	}

	// 检查是否有子类型
	parts := strings.SplitN(serviceType, "._", 2)
	if len(parts) == 2 {
		serviceSubtype = parts[0]
		if strings.HasPrefix(serviceSubtype, "_") {
			serviceSubtype = serviceSubtype[1:]
		}
		serviceType = "._" + parts[1]
	}

	// 移除尾部的"."
	serviceType = strings.TrimSuffix(serviceType, ".")

	// 移除"local."后缀
	serviceType = strings.TrimSuffix(serviceType, ".local")

	return serviceType, serviceSubtype
}
