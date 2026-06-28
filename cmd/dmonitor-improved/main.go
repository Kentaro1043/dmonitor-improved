package main

import (
	"flag"
	"log"
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
	flag.Parse()

	absRootfs, err := filepath.Abs(*rootfs)
	if err != nil {
		log.Fatalf("resolve rootfs: %v", err)
	}

	manager := runtime.NewManager(runtime.Options{
		RootFS:           absRootfs,
		QEMUPath:         *qemu,
		GuestPreloadPath: *preload,
	})
	defer manager.Shutdown()

	server := app.NewServer(service.New(manager), *staticDir)
	log.Printf("listening on http://%s", *addr)
	if err := http.ListenAndServe(*addr, server.Routes()); err != nil {
		if !os.IsNotExist(err) {
			log.Fatal(err)
		}
	}
}
