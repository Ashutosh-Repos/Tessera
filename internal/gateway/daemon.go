package gateway

import (
	"context"
	"net/http"
	"time"

	"github.com/distributed-transcoder/internal/config"
	"github.com/distributed-transcoder/internal/infra"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
)

// GatewayDaemon is the main gateway process.
type GatewayDaemon struct {
	cfg         config.Config
	state       infra.StateStore
	objectStore infra.ObjectStore
	multiplexer *ProgressMultiplexer
}

func NewGatewayDaemon(cfg config.Config, state infra.StateStore, objectStore infra.ObjectStore) *GatewayDaemon {
	return &GatewayDaemon{
		cfg:         cfg,
		state:       state,
		objectStore: objectStore,
	}
}

// Run is the gateway's main entry point.
func (g *GatewayDaemon) Run(ctx context.Context) error {
	// 1. Initialize Rate Limiter
	rl := NewRateLimiter(g.state, g.cfg.Gateway.RateLimitPerIP, g.cfg.Gateway.RateLimitPerUser)

	// 2. Start Progress Multiplexer
	g.multiplexer = NewProgressMultiplexer(g.state, g.cfg.Gateway.MultiplexBatchMs)
	go g.multiplexer.Run(ctx)

	// 3. Build HTTP Routes
	router := http.NewServeMux()
	router.HandleFunc("POST /api/jobs/upload-session", g.handleCreateSession)
	router.HandleFunc("POST /api/jobs/{uuid}/urls", g.handlePresignedBatch)
	router.HandleFunc("POST /api/jobs/{uuid}/complete", g.handleCompleteUpload)
	router.HandleFunc("GET /api/jobs/{uuid}/uploaded-parts", g.handleListUploadedParts)
	router.HandleFunc("GET /api/jobs/{uuid}/status", g.handleGetStatus)
	router.HandleFunc("GET /progress/{uuid}", g.handleWebSocketOrSSE)
	router.HandleFunc("GET /health", g.handleHealth)

	// Apply rate limiting middleware
	handler := rl.Middleware(router)

	// 4. Start HTTP Server
	srv := &http.Server{
		Addr:         g.cfg.Gateway.ListenAddr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx)
	}()

	// 5. Start Metrics Server
	go func() {
		mux := http.NewServeMux()
		mux.Handle(g.cfg.Metrics.Path, promhttp.Handler())
		metricsSrv := &http.Server{Addr: g.cfg.Metrics.ListenAddr, Handler: mux}
		go func() {
			<-ctx.Done()
			metricsSrv.Shutdown(context.Background())
		}()
		metricsSrv.ListenAndServe()
	}()

	log.Printf("gateway listening on %s", g.cfg.Gateway.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
