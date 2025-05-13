//go:build darwin

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	// "path/filepath" // 如果需要，用于构建 LaunchAgent plist 路径
	// "text/template" // 如果需要，用于生成 plist 文件
	// "log" // 如果需要日志记录
)

// isDarwin 检查当前是否为 macOS 操作系统。
// 在此文件中，它总是返回 true，因为此文件仅在 macOS 上构建。
func isDarwin() bool {
	return true
}

// setupLaunchAgent 是配置 macOS Launch Agent 的占位符。
func setupLaunchAgent() {
	// 在这里实现macOS Launch Agent的配置逻辑
	// 这通常涉及创建一个 plist 文件并将其放置在 ~/Library/LaunchAgents/
	// 然后使用 launchctl load 加载它。
	fmt.Println("Placeholder: Setting up macOS Launch Agent...")

	// 详细的 LaunchAgent 设置逻辑可以放在这里。
	// 例如，创建 plist 文件，写入配置，然后使用 launchctl 加载。
	// 由于这是一个占位符，我们只打印一条消息。
	// 实际实现将需要错误处理和更健壮的路径管理。
}

// daemonize 是 macOS 平台的守护进程化实现。
// 它将尝试配置并加载一个 Launch Agent，或者使用传统的 fork 方法。
func daemonize() {
	// 对于 macOS，守护进程化通常意味着作为 Launch Agent 运行。
	// 首先尝试设置 Launch Agent。
	fmt.Println("Attempting to daemonize for macOS (setup LaunchAgent)...")
	setupLaunchAgent() // 这应该处理将应用配置为在后台运行的逻辑。

	// 在 macOS 上，如果应用程序不是由 launchd（通过 LaunchAgent）启动的，
	// 并且我们希望它在后台运行（例如，在首次运行或手动启动后），
	// 我们可以使用传统的 fork 方法。
	// 但是，通常首选 LaunchAgent，因为它提供了更好的系统集成。

	// 检查一个环境变量，看看是否由 launchd 启动 (这只是一个示例约定)
	// 实际的 LaunchAgent 配置可以设置这样的变量。
	if os.Getenv("LAUNCHED_BY_ECLIP_LAUNCHD") == "" {
		fmt.Println("Not launched by eClip LaunchAgent, considering fork to background...")
		// 只有在没有被 launchd 管理且需要后台运行时才执行 fork。
		// 注意：重复调用此 fork 逻辑（如果 setupLaunchAgent 失败或未完全配置）
		// 可能会导致问题。理想情况下，setupLaunchAgent 应该确保应用被 launchd 管理。

		// 通用fork实现 (仅作为备用或特定场景下的方案)
		// 确保 os.Args[0] 是可执行文件的正确路径
		cmd := exec.Command(os.Args[0], os.Args[1:]...)
		// 可选：设置环境变量以指示子进程是后台进程
		// cmd.Env = append(os.Environ(), "ECLIP_BACKGROUND_PROCESS=1")

		err := cmd.Start()
		if err != nil {
			// log.Printf("Failed to fork process for backgrounding: %v", err)
			fmt.Printf("Failed to fork process for backgrounding: %v\n", err)
			return // 如果 fork 失败，不要退出父进程，让用户看到错误
		}
		// log.Printf("Process forked to background with PID: %d. Exiting parent.", cmd.Process.Pid)
		fmt.Printf("Process forked to background with PID: %d. Exiting parent.\n", cmd.Process.Pid)
		os.Exit(0) // 父进程退出
	} else {
		fmt.Println("Launched by eClip LaunchAgent. No fork needed.")
	}
}
