// Command user-service starts the UserService gRPC server.
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	_ "modernc.org/sqlite"

	userv1 "ms/proto/gen/user/v1"
	"ms/shared/log"
	"ms/user-service/internal/config"
	"ms/user-service/internal/service"
	"ms/user-service/internal/store"
)

func main() {
	cfg := config.Load()
	logger := log.New("user-service", cfg.LogLevel)

	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", "err", err)
		os.Exit(1)
	}

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

	srv := service.New(repo, cfg.JWTSecret, cfg.JWTTTL, logger)

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		logger.Error("listen", "err", err)
		os.Exit(1)
	}

	g := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(g, health.NewServer())
	userv1.RegisterUserServiceServer(g, srv)

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
