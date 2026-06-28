package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"
	"time"

	"github.com/Kentaro1043/dmonitor-improved/internal/installer"
)

func main() {
	rootfs := flag.String("rootfs", filepath.Join("runtime", "rootfs"), "rootfs output path")
	cache := flag.String("cache", filepath.Join("runtime", "cache"), "download cache path")
	skipDeps := flag.Bool("skip-deps", false, "only install the official dmonitor package")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	inst := installer.New(installer.Options{
		RootFS:            *rootfs,
		CacheDir:          *cache,
		InstallDeps:       !*skipDeps,
		VerifyFingerprint: true,
	})
	if err := inst.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
