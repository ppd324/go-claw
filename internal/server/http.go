package server

import (
	"fmt"
	"net/http"

	"go-claw/internal/config"
	"go-claw/internal/dashboard"
)

// HTTPServer represents the HTTP server
type HTTPServer struct {
	cfg        *config.Config
	wsServer   *WebSocketServer
	dashboard  *dashboard.Server
	httpServer *http.Server
}

// NewHTTPServer creates a new HTTP server
func NewHTTPServer(cfg *config.Config, wsServer *WebSocketServer, dash *dashboard.Server) *HTTPServer {
	return &HTTPServer{
		cfg:       cfg,
		wsServer:  wsServer,
		dashboard: dash,
	}
}

// Start starts the HTTP server
func (s *HTTPServer) Start() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Server.Host, s.cfg.Server.Port)

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", s.handleHealth)

	// API endpoints
	mux.HandleFunc("/api/v1/health", s.handleAPIV1Health)

	// WebSocket endpoint
	mux.HandleFunc("/ws", s.handleWebSocketForward)

	// Dashboard
	if s.dashboard != nil {
		s.dashboard.Register(mux)
	}

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	fmt.Printf("HTTP server starting on %s\n", addr)
	fmt.Printf("Dashboard available at http://%s/dashboard\n", addr)
	return s.httpServer.ListenAndServe()
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *HTTPServer) handleAPIV1Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok","version":"1.0.0"}`))
}

func (s *HTTPServer) handleWebSocketForward(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("WebSocket forward request received: %s\n", r.URL.Path)
	s.wsServer.handleWebSocket(w, r)
}
