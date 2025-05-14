package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the application.
type Config struct {
	User    UserConfig    `yaml:"user"`
	Server  ServerConfig  `yaml:"server"`
	History HistoryConfig `yaml:"history"`
	Network NetworkConfig `yaml:"network"`
	Logging LoggingConfig `yaml:"logging"`
}

// UserConfig holds user-specific settings.
type UserConfig struct {
	Username   string `yaml:"username"`
	Password   string `yaml:"password"` // Consider secure storage like keyring
	DeviceName string `yaml:"device_name"`
}

// ServerConfig holds remote sync server settings.
type ServerConfig struct {
	Address string `yaml:"address"`
	Enabled bool   `yaml:"enabled"`
}

// HistoryConfig holds local clipboard history settings.
type HistoryConfig struct {
	Capacity int `yaml:"capacity"`
}

// NetworkConfig holds network-related settings.
type NetworkConfig struct {
	MDNSPort    int  `yaml:"mdns_port"`
	MDNSEnabled bool `yaml:"mdns_enabled"`
}

// LoggingConfig holds logging settings.
type LoggingConfig struct {
	Level    string `yaml:"level"`
	FilePath string `yaml:"file_path"` // If empty, log to stdout or a default location
}

var (
	cfg     *Config
	once    sync.Once
	cfgLock = &sync.Mutex{}
	cfgPath string // Stores the path to the config file
)

const (
	configName = "config.yaml"
	appName    = "eClip"
)

// Init initializes the configuration by loading it from the user's config directory.
// It ensures that the configuration is loaded only once.
func Init() error {
	var initErr error
	once.Do(func() {
		userConfigDir, err := os.UserConfigDir()
		if err != nil {
			initErr = fmt.Errorf("failed to get user config directory: %w", err)
			return
		}

		appConfigDir := filepath.Join(userConfigDir, appName)
		if err = os.MkdirAll(appConfigDir, 0750); err != nil { // 0750: user rwx, group rx, other ---
			initErr = fmt.Errorf("failed to create app config directory '%s': %w", appConfigDir, err)
			return
		}

		cfgPath = filepath.Join(appConfigDir, configName)
		loadedCfg, err := loadConfigFromFile(cfgPath)
		if err != nil {
			initErr = fmt.Errorf("failed to load configuration: %w", err)
			return
		}
		cfg = loadedCfg
	})
	return initErr
}

// loadConfigFromFile loads the configuration from the given path.
// If the file doesn't exist, it creates and returns a default configuration.
func loadConfigFromFile(filePath string) (*Config, error) {
	cfgLock.Lock()
	defer cfgLock.Unlock()

	c := getDefaultConfig()

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fmt.Printf("Configuration file not found at '%s'. Creating with default values.\n", filePath)
		if errSave := saveConfigToFile(filePath, c); errSave != nil {
			return nil, fmt.Errorf("failed to save default config to '%s': %w", filePath, errSave)
		}
		return c, nil
	} else if err != nil {
		return nil, fmt.Errorf("error checking config file '%s': %w", filePath, err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file '%s': %w", filePath, err)
	}

	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config from '%s': %w", filePath, err)
	}
	return c, nil
}

// saveConfigToFile saves the configuration to the given path.
func saveConfigToFile(filePath string, c *Config) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	// Write with 0600 permissions: user rw, group ---, other ---
	return os.WriteFile(filePath, data, 0600)
}

// GetConfig returns the loaded configuration.
// It panics if Init() has not been called successfully.
func GetConfig() *Config {
	if cfg == nil {
		// This typically indicates Init() was not called or failed.
		// For robustness, one might attempt Init() here or return an error.
		// Panicking helps identify programming errors early.
		panic("configuration not initialized; call config.Init() first")
	}
	return cfg
}

// SaveConfig saves the current configuration state to the file.
func SaveConfig() error {
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return fmt.Errorf("cannot save nil configuration; was Init() called?")
	}
	if cfgPath == "" {
		return fmt.Errorf("config path not set; was Init() called successfully?")
	}
	return saveConfigToFile(cfgPath, cfg)
}

// getDefaultConfig returns a Config struct with default values.
func getDefaultConfig() *Config {
	return &Config{
		User: UserConfig{
			Username: "", // To be set on first run
			Password: "", // To be set on first run
		},
		Server: ServerConfig{
			Address: "sync.eclip.example.com:7777", // Default public server
			Enabled: true,
		},
		History: HistoryConfig{
			Capacity: 200,
		},
		Network: NetworkConfig{
			MDNSPort:    5353, // Standard mDNS port
			MDNSEnabled: true,
		},
		Logging: LoggingConfig{
			Level:    "info",
			FilePath: "", // Default: empty, could mean stdout or platform-specific log dir
		},
	}
}

// GetConfigFilePath returns the path to the configuration file.
func GetConfigFilePath() string {
	if cfgPath == "" {
		// This might happen if Init() hasn't been called or failed early.
		// Attempt to determine it, but this is a fallback.
		userConfigDir, err := os.UserConfigDir()
		if err != nil {
			return "" // Cannot determine
		}
		return filepath.Join(userConfigDir, appName, configName)
	}
	return cfgPath
}

// IsUserConfigured checks if essential user details (username, password) are set.
func (c *Config) IsUserConfigured() bool {
	return c.User.Username != "" && c.User.Password != ""
}
