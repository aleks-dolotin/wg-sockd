package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
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

	// Step 0: Check for --version / --dry-run before anything else.
	showVersion := flag.Bool("version", false, "print version and exit")
	dryRun := flag.Bool("dry-run", false, "validate config and prerequisites, then exit")

	// Step 1: Register ALL flags upfront, including config path and config-bound fields.
	// Use defaults initially — they will be overridden by file, then by CLI.
	cfg := config.Defaults()
	configPath := flag.String("config", "/etc/wg-sockd/config.yaml", "config file path")
	serveUIDir := flag.String("serve-ui-dir", "", "serve UI from external directory on TCP (no embed needed)")
	cfg.ApplyFlags(flag.CommandLine)
	flag.Parse()

	if *showVersion {
		v := version
		if buildTags != "" {
			v += "+" + buildTags
		}
		fmt.Printf("wg-sockd %s (commit: %s, built: %s)\n", v, commit, buildDate)
		os.Exit(0)
	}

	log.Println("wg-sockd starting...")

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
	// ServeUI and UIListen: use file value if not explicitly set on CLI.
	if !explicitFlags["serve-ui"] {
		cfg.ServeUI = fileCfg.ServeUI
	}
	if !explicitFlags["ui-listen"] {
		cfg.UIListen = fileCfg.UIListen
	}

	// Step 4: Apply environment variable overrides (4-level config: default → file → env → CLI).
	envApplied, err := cfg.ApplyEnv()
	if err != nil {
		log.Fatalf("FATAL: applying env overrides: %v", err)
	}

	// Step 5: Config override logging — only log non-default values (AC-39).
	defaults := config.Defaults()
	logOverride := func(field, value, source string) {
		log.Printf("Config: %s=%s [%s]", field, value, source)
	}
	if cfg.Interface != defaults.Interface {
		src := "yaml"
		if v, ok := envApplied["WG_SOCKD_INTERFACE"]; ok {
			src = "env: WG_SOCKD_INTERFACE=" + v
		}
		if explicitFlags["interface"] {
			src = "cli"
		}
		logOverride("interface", cfg.Interface, src)
	}
	if cfg.SocketPath != defaults.SocketPath {
		src := "yaml"
		if v, ok := envApplied["WG_SOCKD_SOCKET_PATH"]; ok {
			src = "env: WG_SOCKD_SOCKET_PATH=" + v
		}
		if explicitFlags["socket-path"] {
			src = "cli"
		}
		logOverride("socket_path", cfg.SocketPath, src)
	}
	if cfg.DBPath != defaults.DBPath {
		src := "yaml"
		if v, ok := envApplied["WG_SOCKD_DB_PATH"]; ok {
			src = "env: WG_SOCKD_DB_PATH=" + v
		}
		if explicitFlags["db-path"] {
			src = "cli"
		}
		logOverride("db_path", cfg.DBPath, src)
	}
	if cfg.ServeUI != defaults.ServeUI {
		src := "yaml"
		if v, ok := envApplied["WG_SOCKD_SERVE_UI"]; ok {
			src = "env: WG_SOCKD_SERVE_UI=" + v
		}
		if explicitFlags["serve-ui"] {
			src = "cli"
		}
		logOverride("serve_ui", strconv.FormatBool(cfg.ServeUI), src)
	}
	if cfg.UIListen != defaults.UIListen {
		src := "yaml"
		if v, ok := envApplied["WG_SOCKD_UI_LISTEN"]; ok {
			src = "env: WG_SOCKD_UI_LISTEN=" + v
		}
		if explicitFlags["ui-listen"] {
			src = "cli"
		}
		logOverride("ui_listen", cfg.UIListen, src)
	}

	log.Printf("Config loaded: interface=%s, socket=%s, db=%s", cfg.Interface, cfg.SocketPath, cfg.DBPath)

	// --dry-run: validate config + prerequisites, then exit (AC-14: NO SQLite/socket/wgctrl opened).
	if *dryRun {
		os.Exit(runDryRun(cfg))
	}

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

	// 3. Create wgctrl client (dev mode / degraded mode if fails).
	var wgClient wireguard.WireGuardClient
	if client, ok := maybeDevWgClient(cfg.Interface); ok {
		wgClient = client
	} else {
		wgClient, err = wireguard.NewWgctrlClient()
		if err != nil {
			log.Printf("WARNING: wgctrl init failed: %v — starting in degraded mode", err)
			wgClient = &degradedWgClient{}
		} else {
			defer wgClient.Close()
			log.Println("WireGuard client initialized")
		}
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
	// Config-driven UI with explicit boolean merge (F1, F13).
	// effectiveServeUI is computed from config, then overridden if CLI --serve-ui was explicit.
	effectiveServeUI := cfg.ServeUI
	if explicitFlags["serve-ui"] {
		effectiveServeUI = cfg.ServeUI // already set by flag.Parse → ApplyFlags
	}

	// --serve-ui-dir is a separate mechanism, independent of effectiveServeUI (F13/AC-57).
	var tcpServer *http.Server
	if effectiveServeUI || *serveUIDir != "" {
		uiMux := http.NewServeMux()

		// Mount all API routes on the TCP mux too.
		uiMux.Handle("/api/", mux)

		// Determine static file source.
		var staticFS http.FileSystem
		if *serveUIDir != "" {
			// External directory mode — no build tag needed.
			staticFS = http.Dir(*serveUIDir)
			log.Printf("Serving UI from directory: %s", *serveUIDir)
		} else if effectiveServeUI {
			// Embedded mode — requires embed_ui build tag.
			if embeddedUIFS == nil {
				// AC-6 vs AC-7: config source → warn; CLI source → fatal.
				if explicitFlags["serve-ui"] {
					log.Fatal("FATAL: --serve-ui requires building with -tags embed_ui")
				}
				log.Println("WARNING: serve_ui=true in config but no embedded UI (lean build) — UI disabled")
				effectiveServeUI = false
			} else {
				subFS, err := fs.Sub(*embeddedUIFS, "ui_dist")
				if err != nil {
					log.Fatalf("FATAL: embedded UI fs.Sub: %v", err)
				}
				staticFS = http.FS(subFS)
				log.Println("Serving embedded UI assets")
			}
		}

		// Only start TCP listener if we actually have a UI source.
		if staticFS != nil {
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

			tcpListener, err := net.Listen("tcp", cfg.UIListen)
			if err != nil {
				log.Fatalf("FATAL: TCP listen on %s: %v", cfg.UIListen, err)
			}
			tcpServer = &http.Server{
				Handler:     uiMux,
				ConnContext: middleware.ConnContext,
			}
			go func() {
				log.Printf("UI available at http://%s", cfg.UIListen)
				if err := tcpServer.Serve(tcpListener); err != http.ErrServerClosed {
					log.Printf("TCP server error: %v", err)
				}
			}()
		}
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

// runDryRun validates configuration and prerequisites without starting the agent.
// Returns exit code: 0 = all OK, 1 = fatal issue found.
// AC-14: NO SQLite/socket/wgctrl opened.
func runDryRun(cfg *config.Config) int {
	exitCode := 0

	// 1. Version info.
	v := version
	if buildTags != "" {
		v += "+" + buildTags
	}
	fmt.Printf("wg-sockd %s (commit: %s, built: %s)\n", v, commit, buildDate)
	fmt.Println()

	// 2. Effective config.
	fmt.Println("=== Effective Configuration ===")
	fmt.Printf("  interface:          %s\n", cfg.Interface)
	fmt.Printf("  socket_path:        %s\n", cfg.SocketPath)
	fmt.Printf("  db_path:            %s\n", cfg.DBPath)
	fmt.Printf("  conf_path:          %s\n", cfg.ConfPath)
	fmt.Printf("  serve_ui:           %v\n", cfg.ServeUI)
	fmt.Printf("  ui_listen:          %s\n", cfg.UIListen)
	fmt.Printf("  peer_limit:         %d\n", cfg.PeerLimit)
	fmt.Printf("  reconcile_interval: %s\n", cfg.ReconcileInterval)
	fmt.Printf("  rate_limit:         %d\n", cfg.RateLimit)
	fmt.Println()

	// 3. Prerequisites.
	fmt.Println("=== Prerequisites ===")

	// 3a. WireGuard check — warning only, not fatal (AC-12).
	if _, err := exec.LookPath("wg"); err != nil {
		fmt.Println("  ⚠️  WireGuard tools (wg) not found in PATH")
	} else {
		fmt.Println("  ✅ WireGuard tools found")
	}

	// 3b. ui_listen format validation (F15/AC-53).
	if cfg.ServeUI || cfg.UIListen != "" {
		if _, _, err := net.SplitHostPort(cfg.UIListen); err != nil {
			fmt.Printf("  ❌ invalid ui_listen format: %q — %v\n", cfg.UIListen, err)
			exitCode = 1
		} else {
			fmt.Printf("  ✅ ui_listen format valid: %s\n", cfg.UIListen)
		}
	}

	// 3c. Data directory checks (F5/AC-49/AC-50).
	dbDir := filepath.Dir(cfg.DBPath)
	if info, err := os.Stat(dbDir); err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("  ⚠️  data directory %s does not exist (will be created on first start)\n", dbDir)
		} else {
			fmt.Printf("  ❌ cannot stat data directory %s: %v\n", dbDir, err)
			exitCode = 1
		}
	} else if !info.IsDir() {
		fmt.Printf("  ❌ %s exists but is not a directory\n", dbDir)
		exitCode = 1
	} else {
		// Check writable by attempting to create a temp file.
		tmpFile := filepath.Join(dbDir, ".wg-sockd-dry-run-check")
		if f, err := os.Create(tmpFile); err != nil {
			fmt.Printf("  ❌ data directory %s exists but is not writable: %v\n", dbDir, err)
			exitCode = 1
		} else {
			f.Close()
			os.Remove(tmpFile)
			fmt.Printf("  ✅ data directory writable: %s\n", dbDir)
		}
	}

	// 3d. Socket directory check.
	sockDir := filepath.Dir(cfg.SocketPath)
	if info, err := os.Stat(sockDir); err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("  ⚠️  socket directory %s does not exist (will be created on first start)\n", sockDir)
		} else {
			fmt.Printf("  ❌ cannot stat socket directory %s: %v\n", sockDir, err)
			exitCode = 1
		}
	} else if !info.IsDir() {
		fmt.Printf("  ❌ %s exists but is not a directory\n", sockDir)
		exitCode = 1
	} else {
		fmt.Printf("  ✅ socket directory exists: %s\n", sockDir)
	}

	// 3e. Config file (WireGuard conf) check.
	if _, err := os.Stat(cfg.ConfPath); err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("  ⚠️  WireGuard config %s does not exist\n", cfg.ConfPath)
		} else {
			fmt.Printf("  ❌ cannot stat WireGuard config %s: %v\n", cfg.ConfPath, err)
			exitCode = 1
		}
	} else {
		fmt.Printf("  ✅ WireGuard config exists: %s\n", cfg.ConfPath)
	}

	fmt.Println()
	if exitCode == 0 {
		fmt.Println("✅ Dry run passed — configuration is valid")
	} else {
		fmt.Println("❌ Dry run failed — see errors above")
	}

	return exitCode
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
