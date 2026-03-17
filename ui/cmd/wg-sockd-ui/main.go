// Package main is the entrypoint for wg-sockd-ui, a lightweight Go reverse
// proxy that serves a React SPA and forwards /api/* requests to the wg-sockd
// agent via its Unix domain socket.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aleks-dolotin/wg-sockd/ui/internal/discovery"
	"github.com/aleks-dolotin/wg-sockd/ui/internal/proxy"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	listenAddr := flag.String("listen", ":8080", "HTTP listen address")
	socketPath := flag.String("socket", "/var/run/wg-sockd/wg-sockd.sock", "path to wg-sockd Unix socket")
	webDir := flag.String("web-dir", "./web/dist", "directory to serve static files from")
	flag.Parse()

	log.Printf("wg-sockd-ui starting (listen=%s, socket=%s, web=%s)", *listenAddr, *socketPath, *webDir)

	// Graceful shutdown context.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Socket discovery manager — polls for socket availability, health-checks when connected.
	disc := discovery.New(*socketPath)
	go disc.Run(ctx)

	// Build handler: /api/* → reverse proxy, /ui/status → connection status, everything else → static SPA.
	handler := proxy.NewHandler(*socketPath, *webDir, disc)

	server := &http.Server{
		Addr:    *listenAddr,
		Handler: handler,
	}

	// Shutdown goroutine.
	go func() {
		<-ctx.Done()
		log.Println("Shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("wg-sockd-ui ready on %s", *listenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("FATAL: server error: %v", err)
		os.Exit(1)
	}

	log.Println("wg-sockd-ui stopped")
}

