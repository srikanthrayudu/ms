// Package telemetry provides OpenTelemetry tracing and Prometheus metrics.
package telemetry

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc"
)

// Init initializes OpenTelemetry configuration.
// It sets up the W3C TraceContext propagator for distributed trace context propagation.
func Init(serviceName string) func() {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return func() {}
}

// ServerStatsHandler returns the gRPC stats handler for OpenTelemetry tracing.
func ServerStatsHandler() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler())
}

// ClientStatsHandler returns the gRPC dial option for client-side OpenTelemetry tracing.
func ClientStatsHandler() grpc.DialOption {
	return grpc.WithStatsHandler(otelgrpc.NewClientHandler())
}

// MetricsHandler returns an http.Handler that serves Prometheus metrics.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// StartMetricsServer starts an HTTP listener on addr serving /metrics in the background.
// If addr is empty, it does nothing.
func StartMetricsServer(addr string) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	go func() {
		_ = http.ListenAndServe(addr, mux)
	}()
}
