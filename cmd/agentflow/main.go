package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"
	"github.com/grafana/agent/pkg/flow"

	// Install components
	_ "github.com/grafana/agent/pkg/flow/install"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := interruptContext()
	defer cancel()

	var (
		httpListenAddr = "127.0.0.1:12345"
		configFile     string
	)

	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.StringVar(&httpListenAddr, "server.http-listen-addr", httpListenAddr, "address to listen for http traffic on")
	fs.StringVar(&configFile, "config.file", configFile, "path to config file to load")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return fmt.Errorf("error parsing flags: %w", err)
	}

	// Validate flags
	if configFile == "" {
		return fmt.Errorf("the -config.file flag is required")
	}

	l := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	f := flow.New(l, configFile)

	if err := f.Load(); err != nil {
		return fmt.Errorf("error during the initial gragent load: %w", err)
	}

	// HTTP server
	{
		lis, err := net.Listen("tcp", httpListenAddr)
		if err != nil {
			return fmt.Errorf("failed to listen on %s: %w", httpListenAddr, err)
		}

		r := mux.NewRouter()
		r.Handle("/graph", flow.GraphHandler(f))
		r.Handle("/nametable", flow.NametableHandler(f))
		r.Handle("/config", flow.ConfigHandler(f))

		f.WireRoutes(r)

		r.HandleFunc("/mock/some-password", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintln(w, "example-password")
		})

		go func() {
			defer cancel()

			level.Info(l).Log("msg", "now listening for http traffic", "addr", httpListenAddr)
			if err := http.Serve(lis, r); err != nil {
				level.Info(l).Log("msg", "http server closed", "err", err)
			}
		}()
	}

	// Gragent
	go func() {
		defer cancel()
		if err := f.Run(ctx); err != nil {
			level.Error(l).Log("msg", "error while running gragent", "err", err)
		}
	}()

	<-ctx.Done()
	return nil
}

func interruptContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer cancel()
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		select {
		case <-sig:
		case <-ctx.Done():
		}
		signal.Stop(sig)

		fmt.Fprintln(os.Stderr, "interrupt received")
	}()

	return ctx, cancel
}