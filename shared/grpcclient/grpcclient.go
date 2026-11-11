// Package grpcclient dials gRPC servers with insecure (dev/local) credentials.
package grpcclient

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Dial opens an insecure gRPC client connection to addr.
// Suitable for local development only; swap for TLS in production.
func Dial(addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}
