package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all application configuration.
type Config struct {
	Chain       ChainConfig       `yaml:"chain"`
	Contracts   ContractsConfig   `yaml:"contracts"`
	Curator     CuratorConfig     `yaml:"curator"`
	Detector    DetectorConfig    `yaml:"detector"`
	Persistence PersistenceConfig `yaml:"persistence"`
	Metrics     MetricsConfig     `yaml:"metrics"`
	Logging     LoggingConfig     `yaml:"logging"`
}

// ChainConfig holds blockchain connection settings.
type ChainConfig struct {
	RPCURL  string `yaml:"rpc_url"`
	WSURL   string `yaml:"ws_url"`
	ChainID int64  `yaml:"chain_id"`
}

// ContractsConfig holds smart contract addresses.
type ContractsConfig struct {
	AerodromeFactory string `yaml:"aerodrome_factory"`
}

// CuratorConfig holds pool curation settings.
type CuratorConfig struct {
	TopPoolsCount        int           `yaml:"top_pools_count"`
	ReevaluationInterval time.Duration `yaml:"reevaluation_interval"`
	BootstrapBatchSize   int           `yaml:"bootstrap_batch_size"`
}

// DetectorConfig holds arbitrage detection settings.
type DetectorConfig struct {
	MinProfitFactor float64  `yaml:"min_profit_factor"`
	MaxPathLength   int      `yaml:"max_path_length"`
	NumWorkers      int      `yaml:"num_workers"`
	StartTokens     []string `yaml:"start_tokens"`
}

// PersistenceConfig holds database settings.
type PersistenceConfig struct {
	SQLitePath string `yaml:"sqlite_path"`
}

// MetricsConfig holds Prometheus metrics settings.
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Path    string `yaml:"path"`
}

// LoggingConfig holds logging settings.
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Load reads configuration from a YAML file and applies environment variable overrides.
func Load(path string) (*Config, error) {
	cfg := &Config{}

	// Set defaults
	cfg.setDefaults()

	// Read YAML file if it exists
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if len(data) > 0 {
		// Expand environment variables in YAML content
		expanded := os.ExpandEnv(string(data))
		if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
			return nil, fmt.Errorf("parsing config file: %w", err)
		}
	}

	// Apply environment variable overrides
	cfg.applyEnvOverrides()

	// Validate configuration
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// setDefaults sets default values for all configuration options.
func (c *Config) setDefaults() {
	c.Chain = ChainConfig{
		ChainID: 8453, // Base mainnet
	}
	c.Contracts = ContractsConfig{
		AerodromeFactory: "0x420DD381b31aEf6683db6B902084cB0FFECe40Da",
	}
	c.Curator = CuratorConfig{
		TopPoolsCount:        500,
		ReevaluationInterval: time.Hour,
		BootstrapBatchSize:   100,
	}
	c.Detector = DetectorConfig{
		MinProfitFactor: 1.001,
		MaxPathLength:   6, // Increased from 4 after adding pool reuse prevention
		NumWorkers:      4,
		StartTokens: []string{
			"0x4200000000000000000000000000000000000006", // WETH
			"0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", // USDC
			"0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA", // USDbC
		},
	}
	c.Persistence = PersistenceConfig{
		SQLitePath: "./data/watcher.db",
	}
	c.Metrics = MetricsConfig{
		Enabled: true,
		Port:    8080,
		Path:    "/metrics",
	}
	c.Logging = LoggingConfig{
		Level:  "info",
		Format: "json",
	}
}

// applyEnvOverrides applies environment variable overrides to configuration.
func (c *Config) applyEnvOverrides() {
	// Chain config
	if v := os.Getenv("BASE_RPC_URL"); v != "" {
		c.Chain.RPCURL = v
	}
	if v := os.Getenv("BASE_WS_URL"); v != "" {
		c.Chain.WSURL = v
	}

	// Curator config
	if v := os.Getenv("CURATOR_TOP_POOLS_COUNT"); v != "" {
		var count int
		if _, err := fmt.Sscanf(v, "%d", &count); err == nil && count > 0 {
			c.Curator.TopPoolsCount = count
		}
	}

	// Detector config
	if v := os.Getenv("DETECTOR_MIN_PROFIT_FACTOR"); v != "" {
		var factor float64
		if _, err := fmt.Sscanf(v, "%f", &factor); err == nil && factor > 1.0 {
			c.Detector.MinProfitFactor = factor
		}
	}
	if v := os.Getenv("DETECTOR_MAX_PATH_LENGTH"); v != "" {
		var length int
		if _, err := fmt.Sscanf(v, "%d", &length); err == nil && length >= 2 {
			c.Detector.MaxPathLength = length
		}
	}

	// Metrics config
	if v := os.Getenv("METRICS_PORT"); v != "" {
		var port int
		if _, err := fmt.Sscanf(v, "%d", &port); err == nil && port > 0 {
			c.Metrics.Port = port
		}
	}

	// Persistence config
	if v := os.Getenv("SQLITE_PATH"); v != "" {
		c.Persistence.SQLitePath = v
	}

	// Logging config
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		c.Logging.Level = strings.ToLower(v)
	}
}

// validate checks that all required configuration values are present and valid.
func (c *Config) validate() error {
	if c.Chain.RPCURL == "" {
		return fmt.Errorf("chain.rpc_url is required (set BASE_RPC_URL env var)")
	}
	if c.Chain.WSURL == "" {
		return fmt.Errorf("chain.ws_url is required (set BASE_WS_URL env var)")
	}
	if c.Contracts.AerodromeFactory == "" {
		return fmt.Errorf("contracts.aerodrome_factory is required")
	}
	if c.Curator.TopPoolsCount <= 0 {
		return fmt.Errorf("curator.top_pools_count must be positive")
	}
	if c.Detector.MinProfitFactor <= 1.0 {
		return fmt.Errorf("detector.min_profit_factor must be greater than 1.0")
	}
	if c.Detector.MaxPathLength < 2 {
		return fmt.Errorf("detector.max_path_length must be at least 2")
	}
	if c.Detector.NumWorkers <= 0 {
		return fmt.Errorf("detector.num_workers must be positive")
	}
	if len(c.Detector.StartTokens) == 0 {
		return fmt.Errorf("detector.start_tokens must have at least one token")
	}
	if c.Metrics.Port <= 0 || c.Metrics.Port > 65535 {
		return fmt.Errorf("metrics.port must be a valid port number")
	}
	return nil
}
