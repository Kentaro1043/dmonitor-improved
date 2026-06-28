package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Kentaro1043/dmonitor-improved/internal/config"
	"github.com/Kentaro1043/dmonitor-improved/internal/device"
	"github.com/Kentaro1043/dmonitor-improved/internal/runtime"
)

type Server struct {
	manager   *runtime.Manager
	staticDir string
}

func NewServer(manager *runtime.Manager, staticDir string) *Server {
	return &Server{manager: manager, staticDir: staticDir}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handlePutConfig)
	mux.HandleFunc("POST /api/runtime/start-rpt-conn", s.handleStartRPTConn)
	mux.HandleFunc("POST /api/runtime/stop-rpt-conn", s.handleStopRPTConn)
	mux.HandleFunc("POST /api/monitor/connect", s.handleConnect)
	mux.HandleFunc("POST /api/monitor/disconnect", s.handleDisconnect)
	mux.HandleFunc("POST /api/repeater/scan/start", s.handleStartScan)
	mux.HandleFunc("POST /api/repeater/scan/stop", s.handleStopScan)
	mux.HandleFunc("POST /api/repeater/update", s.handleUpdateRepeaters)
	mux.HandleFunc("POST /api/buffer/increase", s.handleBufferIncrease)
	mux.HandleFunc("POST /api/buffer/decrease", s.handleBufferDecrease)
	mux.HandleFunc("GET /api/logs", s.handleLogs)
	mux.Handle("/", s.staticHandler())
	return s.localhostOnly(withJSON(mux))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.Load(s.manager.RootFS())
	writeJSON(w, http.StatusOK, map[string]any{
		"runtime":  s.manager.Snapshot(),
		"device":   device.Detect(),
		"udevHint": device.UdevHint(),
		"config":   cfg,
	})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load(s.manager.RootFS())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var cfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, err)
		return
	}
	saved, err := config.Save(s.manager.RootFS(), cfg)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleStartRPTConn(w http.ResponseWriter, r *http.Request) {
	if err := s.manager.StartRPTConn(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.manager.Snapshot())
}

func (s *Server) handleStopRPTConn(w http.ResponseWriter, r *http.Request) {
	if err := s.manager.StopRPTConn(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.manager.Snapshot())
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	var req runtime.Connection
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(req.Address) == "" || strings.TrimSpace(req.AreaCallsign) == "" {
		writeError(w, errors.New("address and areaCallsign are required"))
		return
	}
	req.ConnectCallsign = strings.TrimSpace(req.ConnectCallsign)
	req.Address = strings.TrimSpace(req.Address)
	req.Port = strings.TrimSpace(req.Port)
	req.AreaCallsign = strings.ToUpper(strings.TrimSpace(req.AreaCallsign))
	req.ZoneCallsign = strings.ToUpper(strings.TrimSpace(req.ZoneCallsign))
	if err := s.manager.Connect(r.Context(), req); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.manager.Snapshot())
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := s.manager.Disconnect(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.manager.Snapshot())
}

func (s *Server) handleStartScan(w http.ResponseWriter, r *http.Request) {
	if err := s.manager.StartScan(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.manager.Snapshot())
}

func (s *Server) handleStopScan(w http.ResponseWriter, r *http.Request) {
	if err := s.manager.StopScan(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.manager.Snapshot())
}

func (s *Server) handleUpdateRepeaters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 30*time.Second {
		var cancel func()
		ctx, cancel = contextWithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	if err := s.manager.UpdateRepeaters(ctx); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.manager.Snapshot())
}

func (s *Server) handleBufferIncrease(w http.ResponseWriter, r *http.Request) {
	if err := s.manager.SignalDMonitor(syscall.SIGUSR1); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.manager.Snapshot())
}

func (s *Server) handleBufferDecrease(w http.ResponseWriter, r *http.Request) {
	if err := s.manager.SignalDMonitor(syscall.SIGUSR2); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.manager.Snapshot())
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"logs": s.manager.Logs()})
}

func (s *Server) staticHandler() http.Handler {
	if s.staticDir == "" {
		return http.NotFoundHandler()
	}
	if _, err := os.Stat(filepath.Join(s.staticDir, "index.html")); err != nil {
		return http.NotFoundHandler()
	}
	fs := http.FileServer(http.Dir(s.staticDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(s.staticDir, filepath.Clean(r.URL.Path))
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(s.staticDir, "index.html"))
	})
}

func (s *Server) localhostOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if i := strings.LastIndex(host, ":"); i > -1 {
			host = host[:i]
		}
		if host != "" && host != "localhost" && host != "127.0.0.1" && host != "[::1]" && host != "::1" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "LAN access is disabled by default; bind to localhost or add an explicit reverse proxy policy"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}
