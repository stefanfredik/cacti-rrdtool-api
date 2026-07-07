package main

import (
	"cacti-rrd-api/internal/api"
	"cacti-rrd-api/internal/config"
	"cacti-rrd-api/internal/rrd"
	"context"
	_ "embed"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed web/index.html
var indexHTML []byte

func main() {
	log.Println("Starting Cacti RRD API Bridge...")

	// 1. Load configuration
	cfg, err := config.LoadConfig(os.Args[1:])
	if err != nil {
		log.Fatalf("Failed to load configuration: %s", err)
	}

	// 2. Initialize RRD Client (Real CLI or Demo Mock)
	var rrdClient rrd.RRDClient
	if cfg.DemoMode {
		log.Println("⚠️  Running in DEMO MODE. Simulating RRD measurements without rrdtool.")
		rrdClient = rrd.NewMockClient(cfg.RRDDir)
	} else {
		log.Printf("Running in PRODUCTION MODE. Directing commands to: %s", cfg.RRDToolCommand)
		rrdClient = rrd.NewCLIClient(cfg.RRDToolCommand, cfg.RRDDir, cfg.RRDToolTimeout, cfg.MaxConcurrentRRDTool)
	}

	// 3. Initialize & Start Metrics Cache
	cache := rrd.NewMetricsCache(rrdClient, cfg.RefreshInterval)
	cache.Start(context.Background())
	defer cache.Stop()

	// 4. Create HTTP Handler
	handler := api.NewAPIHandler(rrdClient, cache)

	// 5. Create Frontend Static File Handler
	frontendHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(indexHTML)
	})

	// 6. Setup Router & Middleware Chain
	router := api.SetupRouter(cfg, handler, frontendHandler)

	// 7. Configure Production-Grade HTTP Server with strict timeouts
	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           router,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}

	// Channel to capture operating system signals for graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Start the server in a background goroutine
	go func() {
		if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
			log.Printf("Server listening with TLS (HTTPS) on https://%s", cfg.ListenAddress)
			if err := server.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("TLS Server error: %s", err)
			}
		} else {
			log.Printf("Server listening on http://%s", cfg.ListenAddress)
			if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("Server error: %s", err)
			}
		}
	}()

	log.Printf("RRD files directory: %s", cfg.RRDDir)

	// Block until a signal is received
	sig := <-stop
	log.Printf("Received signal %v. Initiating graceful shutdown...", sig)

	// Graceful shutdown timeout of 15 seconds
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Graceful shutdown failed: %v", err)
	}

	log.Println("Server stopped gracefully. Goodbye!")
}
