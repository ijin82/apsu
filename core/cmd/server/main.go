package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"qsync/core/internal/auth"
	"qsync/core/internal/discovery"
	"qsync/core/internal/server"
	"qsync/core/internal/storage"
)

func main() {
	configDir := flag.String("config", "", "Path to config dir")
	flag.Parse()

	if *configDir == "" {
		home, _ := os.UserHomeDir()
		*configDir = home + "/.config/qsync"
	}

	store, err := storage.NewManager(*configDir)
	if err != nil {
		log.Fatalf("Failed to init storage: %v", err)
	}

	pairingMgr := auth.NewPairingManager(store)
	srv := server.NewServer(store, pairingMgr)
	broadcaster := discovery.NewBroadcaster()

	if err := srv.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	cfg := store.GetConfig()
	_ = broadcaster.Start(cfg.ServerName, srv.Port())

	fmt.Printf("qSync core server running on port %d\n", srv.Port())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	broadcaster.Stop()
	_ = srv.Stop()
}
