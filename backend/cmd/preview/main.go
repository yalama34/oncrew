package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/yalama34/orchestrator/internal/preview"
)

func main() {
	appOrigin := os.Getenv("ONCREW_ORIGIN")
	if appOrigin == "" {
		appOrigin = "http://localhost:3000"
	}
	publicOrigin := os.Getenv("PREVIEW_PUBLIC_ORIGIN")
	if publicOrigin == "" {
		publicOrigin = "http://127.0.0.1:8081"
	}
	service, err := preview.NewService(appOrigin, publicOrigin)
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			service.Sweep()
		}
	}()
	controlAddr := os.Getenv("PREVIEW_CONTROL_ADDR")
	if controlAddr == "" {
		controlAddr = "127.0.0.1:8082"
	}
	go serve(controlAddr, service.ControlHandler())
	serve(":8081", service.PublicHandler())
}

func serve(addr string, handler http.Handler) {
	server := &http.Server{
		Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second,
	}
	log.Printf("[PREVIEW] Listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}
