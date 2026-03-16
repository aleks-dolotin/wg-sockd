package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/aleks-dolotin/wg-sockd/agent/internal/api"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/config"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/confwriter"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/health"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/middleware"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/reconciler"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/sockmon"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/storage"
	"github.com/aleks-dolotin/wg-sockd/agent/internal/wireguard"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("wg-sockd starting...")

	// Step 1: Register ALL flags upfront, including config path and config-bound fields.
	// Use defaults initially — they will be overridden by file, then by CLI.
	cfg := config.Defaults()
	configPath := flag.String("config", "/etc/wg-sockd/config.yaml", "config file path")
	serveUI := flag.Bool("serve-ui", false, "serve embedded UI on TCP (requires embed_ui build tag)")
	serveUIDir := flag.String("serve-ui-dir", "", "serve UI from external directory on TCP (no embed needed)")
	uiListenAddr := flag.String("ui-listen", "127.0.0.1:8080", "TCP listen address for UI mode (default: loopback only)")
	cfg.ApplyFlags(flag.CommandLine)
	flag.Parse()

	// Step 2: Load config file — overrides defaults for fields present in YAML.
	fileCfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("FATAL: loading config: %v", err)
	}

	// Step 3: Merge: for each flag NOT explicitly set on CLI, use the file value.
	// flag.Visit visits only flags that were explicitly set.
	explicitFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { explicitFlags[f.Name] = true })

	if !explicitFlags["interface"] {
		cfg.Interface = fileCfg.Interface
	}
	if !explicitFlags["socket-path"] {
		cfg.SocketPath = fileCfg.SocketPath
	}
	if !explicitFlags["db-path"] {
		cfg.DBPath = fileCfg.DBPath
	}
	if !explicitFlags["conf-path"] {
		cfg.ConfPath = fileCfg.ConfPath
	}
	if !explicitFlags["listen-addr"] {
		cfg.ListenAddr = fileCfg.ListenAddr
	}
	if !explicitFlags["auto-approve-unknown"] {
		cfg.AutoApproveUnknown = fileCfg.AutoApproveUnknown
	}
	// These fields are not CLI flags — always take from file.
	cfg.PeerLimit = fileCfg.PeerLimit
	cfg.ReconcileInterval = fileCfg.ReconcileInterval
	cfg.PeerProfiles = fileCfg.PeerProfiles
	cfg.ExternalEndpoint = fileCfg.ExternalEndpoint
	cfg.RateLimit = fileCfg.RateLimit

	log.Printf("Config loaded: interface=%s, socket=%s, db=%s", cfg.Interface, cfg.SocketPath, cfg.DBPath)

	if cfg.AutoApproveUnknown {
		log.Println("WARNING: auto_approve_unknown is enabled — unknown peers will NOT be blocked")
	}

	// Graceful shutdown context.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// 1b. Run SQLite recovery chain if database is corrupted (FM-2).
	parseComments := func(path string) (map[string]storage.PeerMeta, error) {
		meta, err := confwriter.ParseConfComments(path)
		if err != nil {
			return nil, err
		}
		result := make(map[string]storage.PeerMeta, len(meta))
		for k, v := range meta {
			result[k] = storage.PeerMeta{Name: v.Name, Notes: v.Notes}
		}
		return result, nil
	}
	recoveredFrom, err := storage.RecoverDB(cfg.DBPath, cfg.ConfPath, parseComments)
	if err != nil {
		log.Printf("WARNING: recovery chain error: %v", err)
	}
	if recoveredFrom != "" {
		log.Printf("WARN: database recovered from: %s", recoveredFrom)
	}

	// 2. Open SQLite.
	db, err := storage.NewDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("FATAL: opening database: %v", err)
	}
	defer db.Close()
	log.Println("SQLite initialized")

	// 2a. Start hourly backup loop (first backup after 5 minutes).
	go db.BackupLoop(ctx, cfg.DBPath, 5*time.Minute)

	// 2b. Seed peer profiles from config (first start only).
	if len(cfg.PeerProfiles) > 0 {
		seeds := make([]storage.ProfileSeed, len(cfg.PeerProfiles))
		for i, p := range cfg.PeerProfiles {
			seeds[i] = storage.ProfileSeed{
				Name:        p.Name,
				DisplayName: p.DisplayName,
				AllowedIPs:  p.AllowedIPs,
				ExcludeIPs:  p.ExcludeIPs,
				Description: p.Description,
			}
		}
		if err := db.SeedProfiles(seeds); err != nil {
			log.Fatalf("FATAL: seeding profiles: %v", err)
		}
		log.Printf("Profile seeding checked (%d profiles configured)", len(seeds))
	}

	// 3. Create wgctrl client (degraded mode if fails).
	var wgClient wireguard.WireGuardClient
	wgClient, err = wireguard.NewWgctrlClient()
	if err != nil {
		log.Printf("WARNING: wgctrl init failed: %v — starting in degraded mode", err)
		wgClient = &degradedWgClient{}
	} else {
		defer wgClient.Close()
		log.Println("WireGuard client initialized")
	}

	// 4. Create shared config writer — single mutex serialising all wg0.conf writes
	// across both the API handlers and the Reconciler goroutine.
	cw := confwriter.NewSharedWriter()

	// 5. Run initial reconciliation.
	rec := reconciler.New(wgClient, db, cfg, cw)
	if err := rec.ReconcileOnce(ctx); err != nil {
		log.Printf("WARNING: initial reconciliation failed: %v", err)
	} else {
		log.Println("Initial reconciliation complete")
	}

	// 5b. Start periodic reconciliation loop.
	go rec.RunLoop(ctx, cfg.ReconcileInterval)

	// 5c. Start disk space monitor for graceful degradation (FM-6).
	diskChecker := health.NewDiskChecker(filepath.Dir(cfg.DBPath), health.DefaultDiskThreshold)
	go diskChecker.RunLoop(ctx.Done())

	// 6a. Create DebouncedWriter wrapping SharedWriter (Story 5.3).
	// getPeers callback fetches current enabled peers at flush time.
	dw := confwriter.NewDebouncedWriter(cw, cfg.ConfPath, 100*time.Millisecond, func() []confwriter.PeerConf {
		dbPeers, err := db.ListPeers()
		if err != nil {
			log.Printf("ERROR: debounced getPeers failed: %v", err)
			return nil
		}
		peers := make([]confwriter.PeerConf, 0, len(dbPeers))
		for _, p := range dbPeers {
			if !p.Enabled {
				continue
			}
			peers = append(peers, confwriter.PeerConf{
				PublicKey:    p.PublicKey,
				AllowedIPs:   p.AllowedIPs,
				FriendlyName: p.FriendlyName,
				CreatedAt:    p.CreatedAt,
				Notes:        p.Notes,
			})
		}
		return peers
	})

	// 6b. Create handlers + router with rate limiting and read-only guard.
	handlers := api.NewHandlers(wgClient, db, cfg, cw, dw, rec)
	var rl *middleware.RateLimiter
	if cfg.RateLimit > 0 {
		rl = middleware.NewRateLimiter(float64(cfg.RateLimit), cfg.RateLimit)
		log.Printf("Rate limiting enabled: %d req/s per connection", cfg.RateLimit)
	}
	mux := api.NewRateLimitedRouter(handlers, rl, diskChecker)

	// 7. Remove stale socket.
	if err := os.Remove(cfg.SocketPath); err != nil && !os.IsNotExist(err) {
		log.Printf("WARNING: removing stale socket: %v", err)
	}

	// 8. Create socket directory.
	if err := os.MkdirAll(filepath.Dir(cfg.SocketPath), 0750); err != nil {
		log.Fatalf("FATAL: creating socket directory: %v", err)
	}

	// 9. Create socket monitor with self-healing (FM-3).
	sm := sockmon.New(cfg.SocketPath, mux, middleware.ConnContext)
	if err := sm.Start(); err != nil {
		log.Fatalf("FATAL: starting socket server: %v", err)
	}

	// 9a. Start socket self-healing monitor goroutine.
	go sm.RunMonitor(ctx)

	// 10. sd_notify ready.
	if ok, err := daemon.SdNotify(false, daemon.SdNotifyReady); err != nil {
		log.Printf("sd_notify error (ok if not under systemd): %v", err)
	} else if ok {
		log.Println("sd_notify: READY=1 sent")
	}

	// 11. Start watchdog goroutine.
	go watchdogLoop(ctx)


	// 12. Optional: TCP listener for embedded UI mode.
	var tcpServer *http.Server
	if *serveUI || *serveUIDir != "" {
		uiMux := http.NewServeMux()

		// Mount all API routes on the TCP mux too.
		uiMux.Handle("/api/", mux)

		// Determine static file source.
		var staticFS http.FileSystem
		if *serveUIDir != "" {
			// External directory mode — no build tag needed.
			staticFS = http.Dir(*serveUIDir)
			log.Printf("Serving UI from directory: %s", *serveUIDir)
		} else if *serveUI {
			// Embedded mode — requires embed_ui build tag.
			if embeddedUIFS == nil {
				log.Fatal("FATAL: --serve-ui requires building with -tags embed_ui")
			}
			subFS, err := fs.Sub(*embeddedUIFS, "ui_dist")
			if err != nil {
				log.Fatalf("FATAL: embedded UI fs.Sub: %v", err)
			}
			staticFS = http.FS(subFS)
			log.Println("Serving embedded UI assets")
		}

		// SPA fallback handler: /api/* → API, everything else → static or index.html.
		uiMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// RT-4: path.Clean on all requests.
			cleanPath := path.Clean(r.URL.Path)

			// Skip API routes — already handled above.
			if strings.HasPrefix(cleanPath, "/api/") {
				http.NotFound(w, r)
				return
			}

			// Try serving the static file.
			f, err := staticFS.Open(cleanPath)
			if err == nil {
				f.Close()
				http.FileServer(staticFS).ServeHTTP(w, r)
				return
			}

			// SPA fallback: serve index.html for all non-file paths.
			r.URL.Path = "/"
			http.FileServer(staticFS).ServeHTTP(w, r)
		})

		tcpListener, err := net.Listen("tcp", *uiListenAddr)
		if err != nil {
			log.Fatalf("FATAL: TCP listen on %s: %v", *uiListenAddr, err)
		}
		tcpServer = &http.Server{
			Handler:     uiMux,
			ConnContext: middleware.ConnContext,
		}
		go func() {
			log.Printf("UI available at http://%s", *uiListenAddr)
			if err := tcpServer.Serve(tcpListener); err != http.ErrServerClosed {
				log.Printf("TCP server error: %v", err)
			}
		}()
	}

	// Shutdown goroutine — signals completion via shutdownDone channel.
	shutdownDone := make(chan struct{})
	go func() {
		<-ctx.Done()
		log.Println("Shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Flush pending debounced conf writes before closing servers.
		dw.Close()

		sm.Shutdown(shutdownCtx)
		if tcpServer != nil {
			tcpServer.Shutdown(shutdownCtx)
		}

		// Stop rate limiter cleanup goroutine (Finding 1).
		if rl != nil {
			rl.Close()
		}

		close(shutdownDone)
	}()

	// Block until context is cancelled (signal received).
	log.Println("wg-sockd ready")
	<-ctx.Done()

	// Wait for shutdown goroutine to complete (Finding 3 — replaces time.Sleep hack).
	<-shutdownDone
	log.Println("wg-sockd stopped")
}

// watchdogLoop sends WATCHDOG=1 to systemd at half the watchdog interval.
func watchdogLoop(ctx context.Context) {
	usecStr := os.Getenv("WATCHDOG_USEC")
	if usecStr == "" {
		return // not running under systemd watchdog
	}

	usec, err := strconv.ParseInt(usecStr, 10, 64)
	if err != nil || usec <= 0 {
		return
	}

	interval := time.Duration(usec) * time.Microsecond / 2
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			daemon.SdNotify(false, daemon.SdNotifyWatchdog)
		}
	}
}

// degradedWgClient is a no-op WireGuard client for degraded mode.
type degradedWgClient struct{}

func (d *degradedWgClient) GetDevice(name string) (*wireguard.Device, error) {
	return nil, errDegraded
}
func (d *degradedWgClient) ConfigurePeers(name string, peers []wireguard.PeerConfig) error {
	return errDegraded
}
func (d *degradedWgClient) RemovePeer(name string, pubKey wgtypes.Key) error {
	return errDegraded
}
func (d *degradedWgClient) GenerateKeyPair() (wgtypes.Key, wgtypes.Key, error) {
	return wgtypes.Key{}, wgtypes.Key{}, errDegraded
}
func (d *degradedWgClient) Close() error { return nil }

var errDegraded = &degradedError{}

type degradedError struct{}

func (e *degradedError) Error() string { return "wireguard client in degraded mode" }
