// Command payment-service starts the PaymentService gRPC server and
// an optional NATS subscriber that auto-charges rides on completion.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
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

	paymentv1 "ms/proto/gen/payment/v1"
	"ms/shared/events"
	mslog "ms/shared/log"
	"ms/payment-service/internal/config"
	"ms/payment-service/internal/service"
	"ms/payment-service/internal/store"
)

func main() {
	cfg := config.Load()
	logger := mslog.New("payment-service", cfg.LogLevel)

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

	// NATS: optional, retry briefly then continue.
	nc, err := nats.Connect(cfg.NATSURL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(5),
		nats.ReconnectWait(500*time.Millisecond),
		nats.Timeout(2*time.Second),
	)
	if err != nil {
		logger.Warn("connect to NATS, continuing without it", "err", err)
		nc = nil
	} else {
		defer nc.Close()
		// Subscribe to ride completion events and auto-charge.
		if _, err := nc.Subscribe("rides.*.completed", func(m *nats.Msg) {
			var ev events.RideEvent
			if err := json.Unmarshal(m.Data, &ev); err != nil {
				logger.Warn("parse ride event", "err", err)
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := repo.Create(ctx, &store.Payment{
				ID:        "pay_nats_" + ev.RideID[:8],
				RideID:    ev.RideID,
				DriverID:  ev.DriverID,
				Amount:    ev.Fare,
				Status:    int(paymentv1.PaymentStatus_COMPLETED),
				CreatedAt: time.Now().Unix(),
				SettledAt: time.Now().Unix(),
				Notes:     "auto-charged via NATS event",
			}); err != nil {
				logger.Warn("auto-charge ride", "ride_id", ev.RideID, "err", err)
			} else {
				logger.Info("auto-charged ride", "ride_id", ev.RideID, "fare", ev.Fare)
			}
		}); err != nil {
			logger.Warn("subscribe rides.*.completed", "err", err)
		}
	}

	srv := service.New(repo, logger)

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		logger.Error("listen", "err", err)
		os.Exit(1)
	}

	g := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(g, health.NewServer())
	paymentv1.RegisterPaymentServiceServer(g, srv)

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
	return db, db.PingContext(ctx)
}
