package first_run

import (
	"bufio"
	"eClip/internal/config"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	firstRunFlagFile = ".first_run_complete"
)

// IsFirstRun checks if this is the first time the application is being run.
// It does this by checking for the existence of a flag file.
func IsFirstRun() bool {
	// Ensure config is initialized to get the correct path
	if err := config.Init(); err != nil {
		// If config init fails, we can't reliably determine the path.
		// Consider this a non-first run or handle error appropriately.
		// For now, assume it's not first run to avoid setup loop on error.
		fmt.Fprintf(os.Stderr, "Error initializing config for first run check: %v\n", err)
		return false
	}
	flagFilePath := filepath.Join(filepath.Dir(config.GetConfigFilePath()), firstRunFlagFile)
	if _, err := os.Stat(flagFilePath); os.IsNotExist(err) {
		return true
	}
	return false
}

// Setup guides the user through the initial configuration process.
// This includes setting up username, password, and an optional remote server.
func Setup() error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Welcome to eClip! This is your first run.")

	fmt.Print("Enter username: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)

	fmt.Print("Enter password: ")
	// In a real application, password input should be masked.
	// For simplicity, we're reading it as plain text here.
	password, _ := reader.ReadString('\n')
	password = strings.TrimSpace(password)

	fmt.Print("Enter device name (leave blank will use hostname): ")
	deviceName, _ := reader.ReadString('\n')
	deviceName = strings.TrimSpace(deviceName)

	fmt.Print("Enter remote server URL (optional, press Enter for default): ")
	remoteServer, _ := reader.ReadString('\n')
	remoteServer = strings.TrimSpace(remoteServer)
	if remoteServer == "" {
		remoteServer = "https://default.eclip.server" // Default server
	}

	cfg := config.GetConfig()
	cfg.User.Username = username
	cfg.User.Password = password // In a real app, hash the password
	cfg.User.DeviceName = deviceName
	cfg.Server.Address = remoteServer
	cfg.Server.Enabled = true // remoteServer will have a value (user input or default)

	if err := config.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save initial configuration: %w", err)
	}

	// Create the flag file to indicate setup is complete
	// Ensure config is initialized to get the correct path for the flag file
	if err := config.Init(); err != nil {
		return fmt.Errorf("error initializing config for creating flag file: %w", err)
	}
	flagFilePath := filepath.Join(filepath.Dir(config.GetConfigFilePath()), firstRunFlagFile)
	f, err := os.Create(flagFilePath)
	if err != nil {
		return fmt.Errorf("failed to create first run flag file: %w", err)
	}
	defer f.Close()

	fmt.Println("Initial setup complete. Configuration saved.")
	return nil
}
