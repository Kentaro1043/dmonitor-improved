package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/Kentaro1043/dmonitor-improved/internal/installer"
)

func main() {
	rootfs := flag.String("rootfs", filepath.Join("runtime", "rootfs"), "rootfs output path")
	cache := flag.String("cache", filepath.Join("runtime", "cache"), "download cache path")
	skipDeps := flag.Bool("skip-deps", false, "only install the official dmonitor package")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, error")
	flag.Parse()

	logger := newLogger(*logLevel)
	logger.Info("installing rootfs", "rootfs", *rootfs, "cache", *cache, "install_deps", !*skipDeps)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	inst := installer.New(installer.Options{
		RootFS:            *rootfs,
		CacheDir:          *cache,
		InstallDeps:       !*skipDeps,
		VerifyFingerprint: true,
	})
	if err := inst.Run(ctx); err != nil {
		logger.Error("install rootfs failed", "error", err)
		os.Exit(1)
	}
	logger.Info("installed rootfs", "rootfs", *rootfs)
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
