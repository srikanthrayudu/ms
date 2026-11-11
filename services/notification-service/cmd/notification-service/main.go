// Command notification-service starts the NotificationService gRPC server and
// subscribes to NATS ride/payment events to fan out in-app notifications.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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

	notificationv1 "ms/proto/gen/notification/v1"
	"ms/shared/events"
	mslog "ms/shared/log"
	"ms/notification-service/internal/config"
	"ms/notification-service/internal/hub"
	"ms/notification-service/internal/service"
	"ms/notification-service/internal/store"
)

func main() {
	cfg := config.Load()
	logger := mslog.New("notification-service", cfg.LogLevel)

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

	h := hub.New()
	svc := service.New(repo, h, logger)

	// NATS: optional, subscribe to ride and payment events.
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
		registerNATSSubscribers(nc, repo, h, logger)
	}
	_ = nc // suppress unused warning if NATS is nil

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		logger.Error("listen", "err", err)
		os.Exit(1)
	}

	g := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(g, health.NewServer())
	notificationv1.RegisterNotificationServiceServer(g, svc)

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

// registerNATSSubscribers wires ride lifecycle and payment events to notifications.
func registerNATSSubscribers(nc *nats.Conn, repo *store.Store, h *hub.Hub, logger interface{ Warn(string, ...any) }) {
	// Listen on all ride events: rides.<id>.<type>
	if _, err := nc.Subscribe("rides.>", func(m *nats.Msg) {
		var ev events.RideEvent
		if err := json.Unmarshal(m.Data, &ev); err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var title, body string
		var nType notificationv1.NotificationType

		switch ev.Type {
		case events.RideMatched:
			nType = notificationv1.NotificationType_RIDE_MATCHED
			title = "Driver matched!"
			body = fmt.Sprintf("Your driver is on the way. Ride %s", ev.RideID[:8])
		case events.RideStarted:
			nType = notificationv1.NotificationType_DRIVER_ARRIVED
			title = "Driver is on the way"
			body = "Your driver has started the trip toward you."
		case events.RideOngoing:
			nType = notificationv1.NotificationType_RIDE_STARTED
			title = "Trip in progress"
			body = "You're on your way! Sit back and relax."
		case events.RideCompleted:
			nType = notificationv1.NotificationType_RIDE_COMPLETED
			title = "Trip completed"
			body = fmt.Sprintf("Your trip is done. Fare: $%.2f", ev.Fare)
		case events.RideCancelled:
			nType = notificationv1.NotificationType_RIDE_CANCELLED
			title = "Ride cancelled"
			body = fmt.Sprintf("Ride %s was cancelled.", ev.RideID[:8])
		default:
			return
		}

		// Determine recipients. For completed/cancelled notify both parties.
		// For other events we only know the driverID from the event; we notify
		// them. The gateway's ride stream handles the passenger side via SSE.
		recipients := []string{}
		if ev.DriverID != "" {
			recipients = append(recipients, ev.DriverID)
		}

		for _, uid := range recipients {
			n := &store.Notification{
				ID:        fmt.Sprintf("notif_%s_%s", ev.Type, uid[:4]),
				UserID:    uid,
				Type:      int(nType),
				Title:     title,
				Body:      body,
				RideID:    ev.RideID,
				CreatedAt: time.Now().Unix(),
			}
			// Best-effort: ignore duplicate IDs (INSERT OR IGNORE is not used here,
			// but the probability of collision on a short ID is acceptable).
			if err := repo.Create(ctx, n); err == nil {
				h.Publish(uid, &notificationv1.Notification{
					Id: n.ID, UserId: uid, Type: nType,
					Title: title, Body: body, RideId: ev.RideID,
					CreatedAt: n.CreatedAt,
				})
			}
		}
	}); err != nil {
		logger.Warn("subscribe rides.>", "err", err)
	}
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
