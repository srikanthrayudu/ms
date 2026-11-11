// Package clients holds gRPC client connections to the backend services.
package clients

import (
	"google.golang.org/grpc"

	driverv1       "ms/proto/gen/driver/v1"
	locationv1     "ms/proto/gen/location/v1"
	notificationv1 "ms/proto/gen/notification/v1"
	paymentv1      "ms/proto/gen/payment/v1"
	ridev1         "ms/proto/gen/ride/v1"
	userv1         "ms/proto/gen/user/v1"
	"ms/shared/grpcclient"
)

// Clients bundles all six service gRPC clients.
type Clients struct {
	User         userv1.UserServiceClient
	Driver       driverv1.DriverServiceClient
	Location     locationv1.LocationServiceClient
	Ride         ridev1.RideServiceClient
	Payment      paymentv1.PaymentServiceClient
	Notification notificationv1.NotificationServiceClient
	conns        []*grpc.ClientConn
}

// Config holds the dial addresses for all services.
type Config struct {
	UserAddr         string
	DriverAddr       string
	LocationAddr     string
	RideAddr         string
	PaymentAddr      string
	NotificationAddr string
}

// New dials all services lazily and returns the client bundle.
func New(cfg Config) (*Clients, error) {
	c := &Clients{}
	dials := []struct {
		addr string
		reg  func(*grpc.ClientConn)
	}{
		{cfg.UserAddr, func(conn *grpc.ClientConn) { c.User = userv1.NewUserServiceClient(conn) }},
		{cfg.DriverAddr, func(conn *grpc.ClientConn) { c.Driver = driverv1.NewDriverServiceClient(conn) }},
		{cfg.LocationAddr, func(conn *grpc.ClientConn) { c.Location = locationv1.NewLocationServiceClient(conn) }},
		{cfg.RideAddr, func(conn *grpc.ClientConn) { c.Ride = ridev1.NewRideServiceClient(conn) }},
		{cfg.PaymentAddr, func(conn *grpc.ClientConn) { c.Payment = paymentv1.NewPaymentServiceClient(conn) }},
		{cfg.NotificationAddr, func(conn *grpc.ClientConn) { c.Notification = notificationv1.NewNotificationServiceClient(conn) }},
	}
	for _, d := range dials {
		if err := c.dial(d.addr, d.reg); err != nil {
			c.Close()
			return nil, err
		}
	}
	return c, nil
}

func (c *Clients) dial(addr string, register func(*grpc.ClientConn)) error {
	conn, err := grpcclient.Dial(addr)
	if err != nil {
		return err
	}
	c.conns = append(c.conns, conn)
	register(conn)
	return nil
}

// Close releases all gRPC connections.
func (c *Clients) Close() {
	for _, conn := range c.conns {
		if conn != nil {
			_ = conn.Close()
		}
	}
}
