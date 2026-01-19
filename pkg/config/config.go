package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	BaseRPCURL    string
	Neo4jURI      string
	Neo4jUsername string
	Neo4jPassword string
}

func Load() (*Config, error) {
	// Load .env file if it exists (ignore error if not found)
	_ = godotenv.Load()

	cfg := &Config{
		BaseRPCURL:    os.Getenv("BASE_RPC_URL"),
		Neo4jURI:      getEnvOrDefault("NEO4J_URI", "neo4j://localhost:7687"),
		Neo4jUsername: getEnvOrDefault("NEO4J_USERNAME", "neo4j"),
		Neo4jPassword: os.Getenv("NEO4J_PASSWORD"),
	}

	if cfg.BaseRPCURL == "" {
		return nil, fmt.Errorf("BASE_RPC_URL environment variable is required")
	}

	if cfg.Neo4jPassword == "" {
		return nil, fmt.Errorf("NEO4J_PASSWORD environment variable is required")
	}

	return cfg, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
