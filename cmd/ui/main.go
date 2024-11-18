package main

import (
	"context"
	"log"
	"net/http"
	"time"
	"watcher/pkg/db"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func main() {
    ctx := context.Background()

    log.Println("Initializing database connection...")
    database, err := db.NewGraphDB(db.Neo4jConfig{
        URI:      "neo4j://localhost:7687",
        Username: "neo4j",
        Password: "your-secure-password",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer database.Close(ctx)

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(w, r, database)
	})

	http.HandleFunc("/", serveHome)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleWebSocket(w http.ResponseWriter, r *http.Request, database *db.GraphDB) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			graphData, err := database.FetchGraphData(r.Context())
			if err != nil {
				log.Printf("Failed to fetch graph data: %v", err)
				continue
			}

			if err := conn.WriteJSON(graphData); err != nil {
				log.Printf("Failed to write to WebSocket: %v", err)
				return
			}
		}
	}
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, "static/index.html")
}