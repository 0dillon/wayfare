// Command wayfared serves the Wayfare corridor monitor over HTTP.
//
//	wayfared -addr :8080
//
// The service is read-only. It holds no keys, signs nothing, and moves no
// funds; every request it serves is a live measurement of public data.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Wayfare-labs/wayfare/api"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
)

func main() {
	var (
		addr    = flag.String("addr", ":8080", "listen address")
		horizon = flag.String("horizon", "", "Horizon base URL (default: mainnet)")
		timeout = flag.Duration("timeout", 90*time.Second, "per-corridor measurement timeout")
	)
	flag.Parse()

	srv := &api.Server{
		Engine: &route.Engine{
			DEX:     &dex.Client{HorizonURL: *horizon},
			RefRate: &refrate.Checked{Inner: &refrate.ExchangeRateAPI{}},
		},
		Timeout: *timeout,
	}

	httpSrv := &http.Server{
		Addr:    *addr,
		Handler: srv.Handler(),

		// A corridor measurement is a dozen sequential round trips to
		// Horizon, so the write timeout has to exceed the measurement
		// timeout or the response is cut off mid-flight.
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      *timeout + 30*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	idle := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Printf("shutdown: %v", err)
		}
		close(idle)
	}()

	log.Printf("wayfare monitor listening on %s", *addr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen: %v", err)
	}
	<-idle
}
