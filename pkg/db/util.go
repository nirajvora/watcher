package db

import "os"

func SetConnectionParams() (string, string, string) {
	uri := os.Getenv("NEO4J_URI")
	if uri == "" {
		uri = "bolt://localhost:7687"
	}

	username := os.Getenv("NEO4J_USER")
	if username == "" {
		username = "neo4j"
	}

	password := os.Getenv("NEO4J_PASSWORD")
	if password == "" {
		password = "your-secure-password"
	}

	return username, password, uri
}
