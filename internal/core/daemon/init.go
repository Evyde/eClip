package daemon

// Daemonize initiates the daemonization process.
// The actual implementation is platform-specific and will call
// an unexported daemonize() function defined in platform files
// (init_windows.go, init_darwin.go, init_other.go).
func Daemonize() {
	// This unexported function will be provided by platform-specific files.
	daemonize()
}
