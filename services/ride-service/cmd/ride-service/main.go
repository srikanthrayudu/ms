// Command ride-service starts the RideService gRPC server.
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

	"github.com/nats-io/nats.go"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	_ "modernc.org/sqlite"

	driverv1 "ms/proto/gen/driver/v1"
	locationv1 "ms/proto/gen/location/v1"
	ridev1 "ms/proto/gen/ride/v1"
	"ms/shared/grpcclient"
	"ms/shared/log"
	"ms/ride-service/internal/config"
	"ms/ride-service/internal/service"
	"ms/ride-service/internal/store"
)

func main() {
	cfg := config.Load()
	logger := log.New("ride-service", cfg.LogLevel)

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

	// NATS is optional: retry briefly at startup, then continue without it if unavailable.
	nc, err := nats.Connect(cfg.NATSURL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(5),
		nats.ReconnectWait(500*time.Millisecond),
		nats.Timeout(2*time.Second),
	)
	if err != nil {
		logger.Warn("connect nats", "err", err, "url", cfg.NATSURL)
	}
	if nc != nil {
		defer nc.Close()
	}

	// Downstream gRPC clients use a lazy dial (NewClient), so they succeed
	// even when the target servers are not running yet.
	locConn, err := grpcclient.Dial(cfg.LocationServiceAddr)
	if err != nil {
		logger.Warn("dial location service", "err", err, "addr", cfg.LocationServiceAddr)
	} else {
		defer locConn.Close()
	}
	drvConn, err := grpcclient.Dial(cfg.DriverServiceAddr)
	if err != nil {
		logger.Warn("dial driver service", "err", err, "addr", cfg.DriverServiceAddr)
	} else {
		defer drvConn.Close()
	}

	var locationClient locationv1.LocationServiceClient
	if locConn != nil {
		locationClient = locationv1.NewLocationServiceClient(locConn)
	}
	var driverClient driverv1.DriverServiceClient
	if drvConn != nil {
		driverClient = driverv1.NewDriverServiceClient(drvConn)
	}

	srv := service.New(repo, locationClient, driverClient, nc, logger)

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		logger.Error("listen", "err", err)
		os.Exit(1)
	}

	g := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(g, health.NewServer())
	ridev1.RegisterRideServiceServer(g, srv)

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
