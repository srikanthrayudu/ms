// Command api-gateway starts the HTTP gateway that serves the Datastar UI
// and proxies to the backend gRPC services.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"ms/api-gateway/internal/clients"
	"ms/api-gateway/internal/config"
	"ms/api-gateway/internal/server"
	mslog "ms/shared/log"
)

func main() {
	cfg := config.Load()
	logger := mslog.New("api-gateway", cfg.LogLevel)

	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", "err", err)
		os.Exit(1)
	}

	cl, err := clients.New(clients.Config{
		UserAddr:         cfg.UserServiceAddr,
		DriverAddr:       cfg.DriverServiceAddr,
		LocationAddr:     cfg.LocationServiceAddr,
		RideAddr:         cfg.RideServiceAddr,
		PaymentAddr:      cfg.PaymentServiceAddr,
		NotificationAddr: cfg.NotificationServiceAddr,
	})
	if err != nil {
		logger.Error("dial backend services", "err", err)
		os.Exit(1)
	}
	defer cl.Close()

	nc, err := nats.Connect(cfg.NATSURL,
		nats.Name("api-gateway"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(5),
		nats.ReconnectWait(500*time.Millisecond),
		nats.Timeout(2*time.Second),
	)
	if err != nil {
		logger.Warn("connect to NATS (streams will be unavailable until it is up)", "err", err, "url", cfg.NATSURL)
	} else {
		defer nc.Close()
	}

	app, err := server.New(cfg, cl, nc)
	if err != nil {
		logger.Error("init app", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      app.Routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // SSE streams stay open
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("listen", "err", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
