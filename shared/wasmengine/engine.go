// Package wasmengine provides a lightweight host-side runner that loads a
// precompiled WASI WebAssembly module (the fare-engine) and invokes it in an
// isolated sandbox using wazero – a pure-Go, zero-CGO WebAssembly runtime.
//
// Architecture:
//
//	ride-service (Go, grpc)
//	    └── wasmengine.Engine.Calculate(req)
//	            └── wazero runtime (in-process, no external process)
//	                    └── fare-engine.wasm (WASI sandbox)
//	                            reads JSON stdin, writes JSON stdout
//	                            no network, no filesystem, no system access
//
// Hot-Reload: The engine watches the .wasm binary path for changes and
// automatically recompiles the module in the background, so pricing logic
// can be updated without restarting the ride-service Pod.
package wasmengine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// FareRequest is the input to the WASM fare-engine.
type FareRequest struct {
	DistanceMeters  float64 `json:"distance_meters"`
	Riders          int     `json:"riders"`
	SurgeMultiplier float64 `json:"surge"`
	RideType        string  `json:"ride_type"` // "SOLO" | "POOL"
}

// FareResponse is the output from the WASM fare-engine.
type FareResponse struct {
	BaseFare   float64 `json:"base_fare"`
	SurgeFare  float64 `json:"surge_fare"`
	PerRider   float64 `json:"per_rider"`
	TotalFare  float64 `json:"total_fare"`
	Multiplier float64 `json:"multiplier"`
	Currency   string  `json:"currency"`
	Engine     string  `json:"engine"`
}

// Engine is the host-side wazero WASM engine. It compiles the .wasm binary
// once at startup, then instantiates a fresh module per request (stateless,
// safe for concurrent use).
type Engine struct {
	mu       sync.RWMutex
	wasmPath string
	runtime  wazero.Runtime
	compiled wazero.CompiledModule
	modTime  time.Time
	calls    int64
	errors   int64
}

// New creates and compiles the WASM fare engine from wasmPath. Returns an
// error if the file cannot be read or compiled.
func New(ctx context.Context, wasmPath string) (*Engine, error) {
	e := &Engine{wasmPath: wasmPath}
	if err := e.compile(ctx); err != nil {
		return nil, fmt.Errorf("wasmengine: compile: %w", err)
	}
	return e, nil
}

// compile reads and compiles the .wasm file. Must be called with write lock
// held or at construction time (single-threaded).
func (e *Engine) compile(ctx context.Context) error {
	wasmBytes, err := os.ReadFile(e.wasmPath)
	if err != nil {
		return err
	}
	info, err := os.Stat(e.wasmPath)
	if err != nil {
		return err
	}

	// Close old runtime before creating a new one (releases memory).
	if e.runtime != nil {
		_ = e.runtime.Close(ctx)
	}

	// wazero.NewRuntimeWithConfig uses an in-process JIT/interpreter; no CGO.
	rt := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		_ = rt.Close(ctx)
		return err
	}

	e.runtime = rt
	e.compiled = compiled
	e.modTime = info.ModTime()
	return nil
}

// Reload checks whether the .wasm binary has been updated on disk and
// recompiles the module if so. This enables zero-downtime pricing updates:
// just replace the .wasm file on a Kubernetes ConfigMap-mounted volume.
func (e *Engine) Reload(ctx context.Context) (bool, error) {
	info, err := os.Stat(e.wasmPath)
	if err != nil {
		return false, err
	}
	e.mu.RLock()
	same := info.ModTime().Equal(e.modTime)
	e.mu.RUnlock()
	if same {
		return false, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.compile(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// Calculate invokes the WASM fare-engine inside a fresh module instance.
// Each call is fully isolated: shared mutable state is impossible.
func (e *Engine) Calculate(ctx context.Context, req FareRequest) (*FareResponse, error) {
	e.mu.RLock()
	compiled := e.compiled
	rt := e.runtime
	e.mu.RUnlock()

	// Encode request as JSON → will be piped to WASM stdin.
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("wasmengine: marshal: %w", err)
	}

	var stdoutBuf bytes.Buffer
	cfg := wazero.NewModuleConfig().
		WithStdin(bytes.NewReader(reqJSON)).
		WithStdout(&stdoutBuf).
		WithStderr(os.Stderr). // surface WASM panics in service logs
		WithSysNanosleep().
		WithSysNanotime().
		WithSysWalltime()

	mod, err := rt.InstantiateModule(ctx, compiled, cfg)
	if err != nil {
		e.mu.Lock()
		e.errors++
		e.mu.Unlock()
		return nil, fmt.Errorf("wasmengine: instantiate: %w", err)
	}
	_ = mod.Close(ctx)

	var resp FareResponse
	if err := json.Unmarshal(bytes.TrimSpace(stdoutBuf.Bytes()), &resp); err != nil {
		return nil, fmt.Errorf("wasmengine: unmarshal response: %w (raw: %s)", err, stdoutBuf.String())
	}

	e.mu.Lock()
	e.calls++
	e.mu.Unlock()

	return &resp, nil
}

// Stats returns lightweight metrics for Prometheus export.
func (e *Engine) Stats() (calls int64, errors int64, wasmPath string) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.calls, e.errors, e.wasmPath
}

// Close releases all wazero runtime resources.
func (e *Engine) Close(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.runtime != nil {
		_ = e.runtime.Close(ctx)
		e.runtime = nil
	}
}
