package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/infrastructure/sims/internal/telemetry"
)

// ReadinessCheck is a function returning nil if a component is ready, or an error otherwise.
type ReadinessCheck func(ctx context.Context) error

// Server wraps an HTTP server with health, readiness, and metrics endpoints.
type Server struct {
	ServiceName     string
	Version         string
	Port            string
	Logger          *slog.Logger
	Metrics         *telemetry.Metrics
	Mux             *http.ServeMux
	readinessChecks map[string]ReadinessCheck
	middlewares     []func(http.Handler) http.Handler
}

// New creates a new Server instance.
func New(serviceName, version, port string, logger *slog.Logger) *Server {
	metrics := telemetry.NewMetrics(serviceName, version)
	mux := http.NewServeMux()

	s := &Server{
		ServiceName:     serviceName,
		Version:         version,
		Port:            port,
		Logger:          logger,
		Metrics:         metrics,
		Mux:             mux,
		readinessChecks: make(map[string]ReadinessCheck),
	}

	s.setupCoreRoutes()
	return s
}

// Use adds a middleware to the server handler chain.
func (s *Server) Use(mw func(http.Handler) http.Handler) {
	s.middlewares = append(s.middlewares, mw)
}

// Handler returns the HTTP handler wrapped with registered middlewares and metrics.
func (s *Server) Handler() http.Handler {
	var h http.Handler = s.Mux
	for i := len(s.middlewares) - 1; i >= 0; i-- {
		h = s.middlewares[i](h)
	}
	return s.Metrics.Middleware(h)
}

// AddReadinessCheck registers a named dependency readiness check.
func (s *Server) AddReadinessCheck(name string, check ReadinessCheck) {
	s.readinessChecks[name] = check
}

func (s *Server) setupCoreRoutes() {
	s.Mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.Mux.HandleFunc("GET /readyz", s.handleReadyz)
	s.Mux.Handle("GET /metrics", s.Metrics.Handler())
}

type healthResponse struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Service   string            `json:"service"`
	Version   string            `json:"version"`
	Details   map[string]string `json:"details,omitempty"`
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	resp := healthResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Service:   s.ServiceName,
		Version:   s.Version,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	details := make(map[string]string)
	allReady := true

	for name, check := range s.readinessChecks {
		if err := check(ctx); err != nil {
			allReady = false
			details[name] = fmt.Sprintf("unready: %v", err)
		} else {
			details[name] = "ok"
		}
	}

	statusCode := http.StatusOK
	statusStr := "ok"
	if !allReady {
		statusCode = http.StatusServiceUnavailable
		statusStr = "degraded"
	}

	resp := healthResponse{
		Status:    statusStr,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Service:   s.ServiceName,
		Version:   s.Version,
		Details:   details,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(resp)
}

// Run starts the HTTP server and blocks until a SIGINT/SIGTERM is received, performing graceful shutdown.
func (s *Server) Run() error {
	addr := fmt.Sprintf(":%s", s.Port)
	handler := s.Handler()

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errChan := make(chan error, 1)
	go func() {
		s.Logger.Info("starting HTTP server", slog.String("service", s.ServiceName), slog.String("addr", addr))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errChan:
		return fmt.Errorf("server error: %w", err)
	case sig := <-sigChan:
		s.Logger.Info("shutdown signal received", slog.String("signal", sig.String()))
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			s.Logger.Error("graceful shutdown failed", slog.String("error", err.Error()))
			return err
		}
		s.Logger.Info("server exited cleanly")
		return nil
	}
}
