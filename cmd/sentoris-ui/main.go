package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sentoris-ai/sentoris-proxy/internal/ui"
	"github.com/sentoris-ai/sentoris-proxy/internal/ui/api"
)

func main() {
	port := flag.Int("port", 8083, "UI Server port")
	proxyURL := flag.String("proxy-url", "http://localhost:8082", "Sentoris Proxy API URL")
	staticDir := flag.String("static-dir", "./web", "Static files directory")
	flag.Parse()

	log.Printf("Sentoris UI starting on port %d...", *port)
	log.Printf("Proxy API URL: %s", *proxyURL)
	log.Printf("Static files directory: %s", *staticDir)

	apiClient := api.NewClient(*proxyURL)
	server := ui.NewServer(apiClient, *staticDir, *proxyURL)

	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", server.ProxyHandler()))
	mux.Handle("/", server.StaticHandler())

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: mux,
	}

	go func() {
		log.Printf("UI Server listening on :%d", *port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down UI server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("UI Server exited gracefully")
}
