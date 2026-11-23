package wasmengine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ms/shared/wasmengine"
)

func wasmPath(t *testing.T) string {
	t.Helper()
	// Try to locate the pre-built fare-engine.wasm from the workspace root.
	// When running via `task test` from /home/user/Projects/ms this resolves.
	candidates := []string{
		filepath.Join("..", "..", "wasm", "fare-engine", "fare-engine.wasm"),
		filepath.Join(os.Getenv("GOPATH"), "src", "ms", "wasm", "fare-engine", "fare-engine.wasm"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("fare-engine.wasm not found; run `task wasm:build` first")
	return ""
}

func TestEngine_SoloFare(t *testing.T) {
	ctx := context.Background()
	e, err := wasmengine.New(ctx, wasmPath(t))
	if err != nil {
		t.Fatalf("wasmengine.New: %v", err)
	}
	defer e.Close(ctx)

	resp, err := e.Calculate(ctx, wasmengine.FareRequest{
		DistanceMeters:  10_000, // 10 km
		Riders:          1,
		SurgeMultiplier: 1.0,
		RideType:        "SOLO",
	})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	// 5 + 1.5*10 = 20.00 USD
	if resp.TotalFare != 20.00 {
		t.Errorf("expected $20.00 solo fare, got $%.2f", resp.TotalFare)
	}
	if resp.Engine != "wasm/fare-engine@v1" {
		t.Errorf("unexpected engine id: %q", resp.Engine)
	}
	t.Logf("Solo 10km $%.2f (engine: %s)", resp.TotalFare, resp.Engine)
}

func TestEngine_SurgedSoloFare(t *testing.T) {
	ctx := context.Background()
	e, err := wasmengine.New(ctx, wasmPath(t))
	if err != nil {
		t.Fatalf("wasmengine.New: %v", err)
	}
	defer e.Close(ctx)

	resp, err := e.Calculate(ctx, wasmengine.FareRequest{
		DistanceMeters:  5_000, // 5 km
		Riders:          1,
		SurgeMultiplier: 2.5, // peak surge cap
		RideType:        "SOLO",
	})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	// base = 5 + 1.5*5 = 12.5; surged = 12.5 * 2.5 = 31.25
	if resp.TotalFare != 31.25 {
		t.Errorf("expected $31.25 surged fare, got $%.2f", resp.TotalFare)
	}
	t.Logf("Surged (2.5x) 5km $%.2f", resp.TotalFare)
}

func TestEngine_PoolFare(t *testing.T) {
	ctx := context.Background()
	e, err := wasmengine.New(ctx, wasmPath(t))
	if err != nil {
		t.Fatalf("wasmengine.New: %v", err)
	}
	defer e.Close(ctx)

	resp, err := e.Calculate(ctx, wasmengine.FareRequest{
		DistanceMeters:  10_000, // 10 km
		Riders:          2,
		SurgeMultiplier: 1.0,
		RideType:        "POOL",
	})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	// Pool per-rider should be less than solo ($20.00)
	if resp.PerRider >= 20.00 {
		t.Errorf("pool per-rider $%.2f should be less than solo $20.00", resp.PerRider)
	}
	t.Logf("Pool 10km 2 riders: $%.2f/rider (total $%.2f)", resp.PerRider, resp.TotalFare)
}

func TestEngine_ConcurrentCalls(t *testing.T) {
	ctx := context.Background()
	e, err := wasmengine.New(ctx, wasmPath(t))
	if err != nil {
		t.Fatalf("wasmengine.New: %v", err)
	}
	defer e.Close(ctx)

	const goroutines = 20
	errc := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(km float64) {
			_, err := e.Calculate(ctx, wasmengine.FareRequest{
				DistanceMeters:  km * 1000,
				Riders:          1,
				SurgeMultiplier: 1.0,
				RideType:        "SOLO",
			})
			errc <- err
		}(float64(i + 1))
	}
	for i := 0; i < goroutines; i++ {
		if err := <-errc; err != nil {
			t.Errorf("concurrent call error: %v", err)
		}
	}

	calls, errors, _ := e.Stats()
	t.Logf("Engine stats: calls=%d errors=%d", calls, errors)
}
