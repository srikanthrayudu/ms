// Command location-service starts the LocationService gRPC server.
package main

import (
	"context"
	"database/sql"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	_ "modernc.org/sqlite"

	locationv1 "ms/proto/gen/location/v1"
	"ms/shared/log"
	"ms/location-service/internal/config"
	"ms/location-service/internal/service"
	"ms/location-service/internal/store"
)

func main() {
	cfg := config.Load()
	logger := log.New("location-service", cfg.LogLevel)

	db, err := openDB(cfg.DBPath)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	repo := store.New(db, logger)
	if err := repo.Migrate(context.Background()); err != nil {
		logger.Error("migrate database", "err", err)
		os.Exit(1)
	}
	if err := repo.LoadAll(context.Background()); err != nil {
		logger.Error("load driver locations", "err", err)
		os.Exit(1)
	}
	logger.Info("loaded driver locations", "count", repo.Count())

	// NATS is optional: retry briefly at startup, then continue without it if unavailable.
	nc, err := nats.Connect(cfg.NATSURL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(5),
		nats.ReconnectWait(500*time.Millisecond),
		nats.Timeout(2*time.Second),
	)
	if err != nil {
		logger.Warn("connect to NATS, continuing without it", "url", cfg.NATSURL, "err", err)
		nc = nil
	}
	if nc != nil {
		defer nc.Close()
	}

	srv := service.New(repo, nc, logger)

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		logger.Error("listen", "err", err)
		os.Exit(1)
	}

	g := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(g, health.NewServer())
	locationv1.RegisterLocationServiceServer(g, srv)

	go func() {
		logger.Info("grpc server listening", "addr", cfg.GRPCAddr)
		if err := g.Serve(lis); err != nil {
			logger.Error("serve", "err", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutting down")
	g.GracefulStop()
}

func openDB(path string) (*sql.DB, error) {
	// Ensure the database directory exists; SQLite can create files but not dirs.
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("libsql", "file:"+path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	return db, nil
}
