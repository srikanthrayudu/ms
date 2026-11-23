// Package geo provides geographic helpers, spatial hexagonal indexing, and dynamic surge pricing.
package geo

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// EarthRadiusMeters is the mean Earth radius used by Haversine.
const EarthRadiusMeters = 6_371_000.0

// Haversine returns the great-circle distance in meters between two
// lat/lng points given in degrees.
func Haversine(lat1, lng1, lat2, lng2 float64) float64 {
	la1 := lat1 * math.Pi / 180
	la2 := lat2 * math.Pi / 180
	dLat := (lat2 - lat1) * math.Pi / 180
	dLng := (lng2 - lng1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(la1)*math.Cos(la2)*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return EarthRadiusMeters * c
}

// SoloFare computes the solo ride fare in USD.
// Formula: $3.00 base + $1.50/km.
func SoloFare(distanceMeters float64) float64 {
	return 3.0 + 1.5*(distanceMeters/1000.0)
}

// SoloFareWithSurge computes the solo fare adjusted by dynamic surge pricing multiplier.
func SoloFareWithSurge(distanceMeters, surge float64) float64 {
	if surge < 1.0 {
		surge = 1.0
	}
	return math.Round(SoloFare(distanceMeters)*surge*100) / 100
}

// PoolFare computes the per-rider fare for a pool trip.
// Formula: soloFare / riders + $0.30/km per extra rider (detour surcharge).
func PoolFare(distanceMeters float64, riders int) float64 {
	if riders <= 0 {
		riders = 1
	}
	solo := SoloFare(distanceMeters)
	shared := solo / float64(riders)
	detourPerRider := 0.30 * (distanceMeters / 1000.0) * float64(riders-1)
	return shared + detourPerRider
}

// PoolFareWithSurge computes the per-rider pool fare adjusted by surge multiplier.
func PoolFareWithSurge(distanceMeters float64, riders int, surge float64) float64 {
	if surge < 1.0 {
		surge = 1.0
	}
	return math.Round(PoolFare(distanceMeters, riders)*surge*100) / 100
}

// ─── Spatial Cell Indexing (Hexagonal Grid) ─────────────────────────────────

const (
	// CellRadiusKm defines the spatial resolution (~1.0 km radius per cell).
	CellRadiusKm   = 1.0
	latDegreePerKm = 1.0 / 111.0
	lngDegreePerKm = 1.0 / 85.0 // approximate for mid-latitudes
)

// CellID computes the deterministic discrete spatial cell for a given coordinate.
// It maps continuous coordinates into a staggered hexagonal grid.
func CellID(lat, lng float64) string {
	row := int(math.Floor(lat / (1.5 * latDegreePerKm)))
	colStep := math.Sqrt(3) * lngDegreePerKm
	colOffset := 0.0
	if math.Abs(float64(row%2)) == 1 {
		colOffset = colStep / 2.0
	}
	col := int(math.Floor((lng - colOffset) / colStep))
	return fmt.Sprintf("hex_%d_%d", row, col)
}

// CellCenter returns the approximate center (lat, lng) of a spatial cell.
func CellCenter(cellID string) (lat, lng float64) {
	var row, col int
	if n, _ := fmt.Sscanf(cellID, "hex_%d_%d", &row, &col); n != 2 {
		return 0, 0
	}
	lat = float64(row) * 1.5 * latDegreePerKm
	colStep := math.Sqrt(3) * lngDegreePerKm
	colOffset := 0.0
	if math.Abs(float64(row%2)) == 1 {
		colOffset = colStep / 2.0
	}
	lng = float64(col)*colStep + colOffset
	return lat, lng
}

// CellPolygon returns the 6 vertices [lat, lng] of a hexagon for Leaflet rendering.
func CellPolygon(cellID string) [][2]float64 {
	centerLat, centerLng := CellCenter(cellID)
	rLat := latDegreePerKm * CellRadiusKm
	rLng := lngDegreePerKm * CellRadiusKm
	poly := make([][2]float64, 6)
	for i := 0; i < 6; i++ {
		angle := float64(i) * math.Pi / 3.0
		poly[i] = [2]float64{
			centerLat + rLat*math.Sin(angle),
			centerLng + rLng*math.Cos(angle),
		}
	}
	return poly
}

// ─── Dynamic Surge Pricing Engine ───────────────────────────────────────────

// SurgeZoneInfo describes an active surge pricing zone.
type SurgeZoneInfo struct {
	CellID      string       `json:"cell_id"`
	CenterLat   float64      `json:"center_lat"`
	CenterLng   float64      `json:"center_lng"`
	Multiplier  float64      `json:"multiplier"`
	DemandCount int          `json:"demand_count"`
	SupplyCount int          `json:"supply_count"`
	Polygon     [][2]float64 `json:"polygon"`
}

// SurgeEngine tracks real-time demand & supply per spatial cell to calculate
// dynamic pricing multipliers based on rider demand vs driver availability.
type SurgeEngine struct {
	mu           sync.RWMutex
	demandWindow time.Duration
	demandEvents map[string][]time.Time
	supplyCounts map[string]int
}

// NewSurgeEngine creates a new SurgeEngine with a specified rolling window.
func NewSurgeEngine(window time.Duration) *SurgeEngine {
	if window <= 0 {
		window = 90 * time.Second
	}
	return &SurgeEngine{
		demandWindow: window,
		demandEvents: make(map[string][]time.Time),
		supplyCounts: make(map[string]int),
	}
}

// RecordDemand records a ride request at (lat, lng) to increase cell demand.
func (s *SurgeEngine) RecordDemand(lat, lng float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cid := CellID(lat, lng)
	now := time.Now()
	cutoff := now.Add(-s.demandWindow)

	var fresh []time.Time
	for _, t := range s.demandEvents[cid] {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	fresh = append(fresh, now)
	s.demandEvents[cid] = fresh
}

// UpdateSupply updates the number of available drivers in cellID.
func (s *SurgeEngine) UpdateSupply(cellID string, count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if count < 0 {
		count = 0
	}
	s.supplyCounts[cellID] = count
}

// GetMultiplier returns the current dynamic surge multiplier for (lat, lng).
// It returns a multiplier between 1.0 (normal) and 2.5 (high demand).
func (s *SurgeEngine) GetMultiplier(lat, lng float64) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	cid := CellID(lat, lng)
	cutoff := time.Now().Add(-s.demandWindow)

	var fresh []time.Time
	for _, t := range s.demandEvents[cid] {
		if t.After(cutoff) {
			fresh = append(fresh, t)
		}
	}
	s.demandEvents[cid] = fresh
	demand := len(fresh)
	supply := s.supplyCounts[cid]
	if supply <= 0 {
		supply = 1
	}

	ratio := float64(demand) / float64(supply)
	if ratio <= 1.2 {
		return 1.0
	}
	m := 1.0 + 0.35*(ratio-1.2)
	if m > 2.5 {
		m = 2.5
	}
	return math.Round(m*10) / 10
}

// GetActiveZones returns all cells currently experiencing surge pricing (> 1.0x).
func (s *SurgeEngine) GetActiveZones() []SurgeZoneInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-s.demandWindow)
	var zones []SurgeZoneInfo

	for cid, timestamps := range s.demandEvents {
		var fresh []time.Time
		for _, t := range timestamps {
			if t.After(cutoff) {
				fresh = append(fresh, t)
			}
		}
		s.demandEvents[cid] = fresh
		demand := len(fresh)
		supply := s.supplyCounts[cid]
		if supply <= 0 {
			supply = 1
		}
		ratio := float64(demand) / float64(supply)
		if ratio > 1.2 {
			m := 1.0 + 0.35*(ratio-1.2)
			if m > 2.5 {
				m = 2.5
			}
			m = math.Round(m*10) / 10
			cLat, cLng := CellCenter(cid)
			zones = append(zones, SurgeZoneInfo{
				CellID:      cid,
				CenterLat:   cLat,
				CenterLng:   cLng,
				Multiplier:  m,
				DemandCount: demand,
				SupplyCount: supply,
				Polygon:     CellPolygon(cid),
			})
		}
	}
	return zones
}
