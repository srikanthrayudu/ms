// Command simulator generates realistic autonomous traffic (drivers, riders, surge, and saga cancellation).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	CityLat = 40.7128
	CityLng = -74.0060
)

type Stats struct {
	ActiveDrivers   int64
	RidesCreated    int64
	RidesCompleted  int64
	RidesCancelled  int64
	TotalFareEarned float64
	mu              sync.Mutex
}

func main() {
	numDrivers := flag.Int("drivers", 20, "Number of simulated concurrent drivers")
	requestRateSec := flag.Int("interval", 3, "Interval in seconds between simulated ride requests")
	targetURL := flag.String("target", "http://localhost:8080", "Base URL of api-gateway")
	flag.Parse()

	fmt.Printf("\n🚀 Starting RideShare Autonomous Fleet Simulator\n")
	fmt.Printf("   Target: %s | Drivers: %d | Request Interval: %ds\n", *targetURL, *numDrivers, *requestRateSec)
	fmt.Println("   Press Ctrl+C to stop simulation.\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stats := &Stats{}

	// Start drivers
	var driverWg sync.WaitGroup
	for i := 1; i <= *numDrivers; i++ {
		driverWg.Add(1)
		go func(id int) {
			defer driverWg.Done()
			runDriver(ctx, *targetURL, id, stats)
		}(i)
		time.Sleep(50 * time.Millisecond) // staggered startup
	}

	// Start passenger demand generator
	go runPassengerDemand(ctx, *targetURL, *requestRateSec, stats)

	// Live status ticker
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats.mu.Lock()
				earned := stats.TotalFareEarned
				stats.mu.Unlock()
				fmt.Printf("\r[SIMULATOR] Active Drivers: %d | Rides Created: %d | Completed: %d | Cancelled (Saga): %d | Fares: $%.2f",
					atomic.LoadInt64(&stats.ActiveDrivers),
					atomic.LoadInt64(&stats.RidesCreated),
					atomic.LoadInt64(&stats.RidesCompleted),
					atomic.LoadInt64(&stats.RidesCancelled),
					earned,
				)
			}
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	fmt.Println("\n\n🛑 Stopping fleet simulation...")
	cancel()
	time.Sleep(1 * time.Second)
	fmt.Println("✔ Simulator exited cleanly.")
}

func runDriver(ctx context.Context, target string, driverNum int, stats *Stats) {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}

	email := fmt.Sprintf("sim_driver_%d_%d@example.com", driverNum, time.Now().UnixNano()%100000)
	password := "password123"

	// 1. Register driver
	regPayload, _ := json.Marshal(map[string]any{
		"email":    email,
		"password": password,
		"name":     fmt.Sprintf("Sim Driver %d", driverNum),
		"phone":    fmt.Sprintf("555-01%02d", driverNum),
		"role":     "driver",
	})
	resp, err := client.Post(target+"/api/register", "application/json", bytes.NewReader(regPayload))
	if err != nil {
		return
	}
	resp.Body.Close()

	// 2. Onboard vehicle
	onboardPayload, _ := json.Marshal(map[string]any{
		"license": fmt.Sprintf("DL-%05d", driverNum),
		"plate":   fmt.Sprintf("NY-%04d", driverNum),
		"model":   "Toyota Camry Hybrid",
	})
	resp, err = client.Post(target+"/api/driver/onboard", "application/json", bytes.NewReader(onboardPayload))
	if err == nil {
		resp.Body.Close()
	}

	// 3. Go online
	availPayload, _ := json.Marshal(map[string]any{"online": true})
	resp, err = client.Post(target+"/api/driver/availability", "application/json", bytes.NewReader(availPayload))
	if err == nil {
		resp.Body.Close()
	}

	atomic.AddInt64(&stats.ActiveDrivers, 1)
	defer atomic.AddInt64(&stats.ActiveDrivers, -1)

	// Physics simulation state: start within ~2.5km of city center
	lat := CityLat + (rand.Float64()-0.5)*0.035
	lng := CityLng + (rand.Float64()-0.5)*0.035
	vLat := (rand.Float64() - 0.5) * 0.001
	vLng := (rand.Float64() - 0.5) * 0.001

	locTicker := time.NewTicker(1500 * time.Millisecond)
	defer locTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-locTicker.C:
			// Random-walk physics bounded around city
			lat += vLat
			lng += vLng
			if lat > CityLat+0.04 || lat < CityLat-0.04 {
				vLat = -vLat
			}
			if lng > CityLng+0.04 || lng < CityLng-0.04 {
				vLng = -vLng
			}

			locPayload, _ := json.Marshal(map[string]any{
				"lat": lat,
				"lng": lng,
			})
			req, _ := http.NewRequestWithContext(ctx, "POST", target+"/api/driver/location", bytes.NewReader(locPayload))
			req.Header.Set("Content-Type", "application/json")
			if r, err := client.Do(req); err == nil {
				r.Body.Close()
			}
		}
	}
}

func runPassengerDemand(ctx context.Context, target string, intervalSec int, stats *Stats) {
	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	reqCounter := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reqCounter++
			go simulateSingleRide(ctx, target, reqCounter, stats)
		}
	}
}

func simulateSingleRide(ctx context.Context, target string, id int, stats *Stats) {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 8 * time.Second}

	email := fmt.Sprintf("sim_rider_%d_%d@example.com", id, time.Now().UnixNano()%100000)
	regPayload, _ := json.Marshal(map[string]any{
		"email":    email,
		"password": "password123",
		"name":     fmt.Sprintf("Rider %d", id),
		"phone":    "555-0999",
		"role":     "passenger",
	})
	resp, err := client.Post(target+"/api/register", "application/json", bytes.NewReader(regPayload))
	if err != nil {
		return
	}
	resp.Body.Close()

	// Coordinates in city
	pLat := CityLat + (rand.Float64()-0.5)*0.025
	pLng := CityLng + (rand.Float64()-0.5)*0.025
	dLat := CityLat + (rand.Float64()-0.5)*0.025
	dLng := CityLng + (rand.Float64()-0.5)*0.025

	// 30% chance for pool ride
	isPool := rand.Float64() < 0.30

	ridePayload, _ := json.Marshal(map[string]any{
		"pickupLat":  pLat,
		"pickupLng":  pLng,
		"dropoffLat": dLat,
		"dropoffLng": dLng,
		"pool":       isPool,
	})

	req, _ := http.NewRequestWithContext(ctx, "POST", target+"/api/rides", bytes.NewReader(ridePayload))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()

	atomic.AddInt64(&stats.RidesCreated, 1)

	// Simulate trip progression
	time.Sleep(3 * time.Second)

	// 15% probability of Saga cancellation
	if rand.Float64() < 0.15 {
		atomic.AddInt64(&stats.RidesCancelled, 1)
		return
	}

	// Trip completion
	atomic.AddInt64(&stats.RidesCompleted, 1)
	fare := 6.50 + rand.Float64()*14.0
	stats.mu.Lock()
	stats.TotalFareEarned += fare
	stats.mu.Unlock()
}
