package ipc

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/landrop/landrop/backend/internal/config"
	"github.com/landrop/landrop/backend/internal/store"
	"github.com/landrop/landrop/backend/internal/transfer"
)

type Server struct {
	config     config.Config
	hub        *Hub
	devices    *store.DeviceRegistry
	transfers  *transfer.Manager
	listener   net.Listener
	httpServer *http.Server
}

func NewServer(cfg config.Config, devices *store.DeviceRegistry, hub *Hub, transfers *transfer.Manager) *Server {
	return &Server{
		config:    cfg,
		hub:       hub,
		devices:   devices,
		transfers: transfers,
	}
}

func (s *Server) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.config.IPCBind)
	if err != nil {
		return err
	}

	s.listener = listener

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/v1/devices", s.handleDevices)
	mux.HandleFunc("/api/v1/transfers", s.handleTransfers)
	mux.HandleFunc("/ws", s.hub.ServeWS)

	s.httpServer = &http.Server{
		Handler:           s.withCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
	}()

	go func() {
		_ = s.httpServer.Serve(listener)
	}()

	return nil
}

func (s *Server) Address() string {
	if s.listener == nil {
		return ""
	}

	return "http://" + s.listener.Addr().String()
}

func (s *Server) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{
		"status":     "ok",
		"deviceId":   s.config.DeviceID,
		"deviceName": s.config.DeviceName,
		"ipc":        s.Address(),
	})
}

func (s *Server) handleDevices(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"items": s.devices.List(),
	})
}

func (s *Server) handleTransfers(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, 1<<20)
	defer request.Body.Close()

	var payload transfer.Request
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	job, err := s.transfers.Queue(payload)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(writer, http.StatusAccepted, job)
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(payload)
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Access-Control-Allow-Origin", "*")
		writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		writer.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")

		if request.Method == http.MethodOptions {
			writer.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(writer, request)
	})
}