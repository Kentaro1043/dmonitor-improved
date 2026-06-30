package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Kentaro1043/dmonitor-improved/internal/app"
	"github.com/Kentaro1043/dmonitor-improved/internal/runtime"
	"github.com/Kentaro1043/dmonitor-improved/internal/service"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	rootfs := flag.String("rootfs", filepath.Join("runtime", "rootfs"), "armhf rootfs path")
	qemu := flag.String("qemu", "qemu-arm", "qemu-arm executable")
	staticDir := flag.String("static", filepath.Join("web", "dist"), "static UI directory")
	preload := flag.String("preload", "/usr/lib/dmonitor-compat.so", "guest LD_PRELOAD path")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, error")
	flag.Parse()

	logger := newLogger(*logLevel)

	absRootfs, err := filepath.Abs(*rootfs)
	if err != nil {
		logger.Error("resolve rootfs", "error", err)
		os.Exit(1)
	}

	manager := runtime.NewManager(runtime.Options{
		RootFS:           absRootfs,
		QEMUPath:         *qemu,
		GuestPreloadPath: *preload,
		Logger:           logger,
	})
	defer manager.Shutdown()
	manager.LogInventory()

	server := app.NewServer(service.New(manager), *staticDir)
	logger.Info("starting server", "addr", *addr, "static_dir", *staticDir)
	if err := http.ListenAndServe(*addr, server.Routes()); err != nil {
		if !os.IsNotExist(err) {
			logger.Error("server stopped", "error", err)
			os.Exit(1)
		}
	}
}

func newLogger(level string) *slog.Logger {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(level)); err != nil {
		parsed = slog.LevelInfo
	}
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: parsed})
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}
