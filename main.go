package main

import (
	"log"
	"net/http"
	"sync"

	"github.com/local/url2mcp/internal/mcp"
	"github.com/local/url2mcp/internal/store"
	"github.com/local/url2mcp/internal/web"
)

func main() {
	st := store.New()

	webHandler := web.NewHandler(st)
	mcpServer := mcp.NewServer(st)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		log.Println("Web UI listening on :8080")
		if err := http.ListenAndServe(":8080", webHandler); err != nil {
			log.Fatalf("web server error: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		log.Println("MCP server listening on :8081")
		if err := http.ListenAndServe(":8081", mcpServer); err != nil {
			log.Fatalf("mcp server error: %v", err)
		}
	}()

	wg.Wait()
}