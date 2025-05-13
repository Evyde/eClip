//go:build !windows && !darwin

package daemon

// 为非Windows和非macOS系统提供默认的空实现
// 这样可以确保在其他平台上编译时不会出错

func isWindows() bool {
	return false
}

func installWindowsService() {
	// 非Windows平台不执行任何操作
}

func isDarwin() bool {
	return false
}

func setupLaunchAgent() {
	// 非macOS平台不执行任何操作
}
