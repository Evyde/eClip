package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/grandcat/zeroconf"
)

func main() {
	instanceName := "MySimpleTestService"
	serviceType := "_mysimpleservice._tcp" // 使用一个独特的服务类型
	domain := "local."
	port := 12345
	txtRecords := []string{"app=SimpleTest", "version=1.0"}

	log.Printf("正在注册服务:\n Instance: %s\n Service:  %s\n Domain:   %s\n Port:     %d\n TXT:      %v\n",
		instanceName, serviceType, domain, port, txtRecords)

	// 尝试让 zeroconf 自动选择接口 (在 Windows 上通常推荐)
	// 您也可以尝试传递特定接口，如果自动选择不起作用
	// interfaces, err := net.Interfaces() // 获取所有接口的示例
	// if err != nil {
	//  log.Fatalf("无法获取接口: %v", err)
	// }
	// server, err := zeroconf.Register(instanceName, serviceType, domain, port, txtRecords, interfaces)

	server, err := zeroconf.Register(instanceName, serviceType, domain, port, txtRecords, nil) // 传递 nil 让库自动选择接口
	if err != nil {
		log.Fatalf("无法注册 mDNS 服务: %v", err)
	}
	defer server.Shutdown()

	log.Printf("服务 %s 已成功注册。按 Ctrl+C 退出。", instanceName)

	// 等待中断信号
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("正在关闭服务...")
}
