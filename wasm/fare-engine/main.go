// Package main is the WASM Fare Engine - a self-contained WASI module that
// reads a FareRequest as JSON from stdin and writes a FareResponse as JSON
// to stdout.  This allows the Go ride-service to run pricing logic inside a
// fully isolated WebAssembly sandbox (via wazero) without any CGO or TinyGo.
//
// Build:
//
//	GOOS=wasip1 GOARCH=wasm go build -o fare-engine.wasm .
package main

import (
	"encoding/json"
	"io"
	"math"
	"os"
)

// FareRequest is the JSON schema read from stdin by the WASM module.
type FareRequest struct {
	DistanceMeters float64 `json:"distance_meters"`
	Riders         int     `json:"riders"`       // 1 = solo, >1 = pool
	SurgeMultiplier float64 `json:"surge"`       // 1.0 = no surge
	RideType       string  `json:"ride_type"`    // "SOLO" | "POOL"
}

// FareResponse is the JSON schema written to stdout by the WASM module.
type FareResponse struct {
	BaseFare   float64 `json:"base_fare"`
	SurgeFare  float64 `json:"surge_fare"`
	PerRider   float64 `json:"per_rider"`
	TotalFare  float64 `json:"total_fare"`
	Multiplier float64 `json:"multiplier"`
	Currency   string  `json:"currency"`
	Engine     string  `json:"engine"`
}

const (
	baseFlagFee    = 5.0  // USD base flag-fall charge
	perKmRate      = 1.5  // USD per km (solo)
	poolDetourRate = 0.30 // USD per km per extra pool passenger detour
	minFare        = 4.0  // USD minimum fare
	maxSurge       = 2.5  // maximum surge multiplier cap
)

// soloFare computes the flat solo fare for a given distance.
func soloFare(distanceMeters float64) float64 {
	km := distanceMeters / 1000.0
	fare := baseFlagFee + perKmRate*km
	if fare < minFare {
		fare = minFare
	}
	return fare
}

// poolFare computes the per-rider pool fare (discounted + detour surcharge).
func poolFare(distanceMeters float64, riders int) float64 {
	if riders <= 1 {
		return soloFare(distanceMeters)
	}
	km := distanceMeters / 1000.0
	solo := soloFare(distanceMeters)
	// Each rider gets a discount from solo fare, but pays a detour surcharge
	// for every additional rider (simulates route deviation cost).
	perRider := (solo / float64(riders)) + poolDetourRate*km*float64(riders-1)
	if perRider < minFare/2 {
		perRider = minFare / 2
	}
	return math.Round(perRider*100) / 100
}

// clampSurge ensures the surge multiplier stays in [1.0, maxSurge].
func clampSurge(surge float64) float64 {
	if surge < 1.0 {
		return 1.0
	}
	if surge > maxSurge {
		return maxSurge
	}
	return surge
}

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil || len(raw) == 0 {
		writeError("failed to read stdin")
		return
	}

	var req FareRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError("invalid JSON input: " + err.Error())
		return
	}

	if req.SurgeMultiplier <= 0 {
		req.SurgeMultiplier = 1.0
	}
	if req.Riders <= 0 {
		req.Riders = 1
	}
	surge := clampSurge(req.SurgeMultiplier)

	var base, perRider, total float64
	if req.RideType == "POOL" && req.Riders > 1 {
		perRider = poolFare(req.DistanceMeters, req.Riders)
		base = soloFare(req.DistanceMeters)
		total = math.Round(perRider*surge*100) / 100
	} else {
		base = soloFare(req.DistanceMeters)
		perRider = base
		total = math.Round(base*surge*100) / 100
	}

	resp := FareResponse{
		BaseFare:   math.Round(base*100) / 100,
		SurgeFare:  math.Round(base*surge*100) / 100,
		PerRider:   perRider,
		TotalFare:  total,
		Multiplier: surge,
		Currency:   "USD",
		Engine:     "wasm/fare-engine@v1",
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "")
	_ = enc.Encode(resp)
}

func writeError(msg string) {
	resp := map[string]string{"error": msg}
	_ = json.NewEncoder(os.Stdout).Encode(resp)
	os.Exit(1)
}
