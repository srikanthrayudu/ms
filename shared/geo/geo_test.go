package geo_test

import (
	"math"
	"testing"

	"ms/shared/geo"
)

func TestHaversine_SamePoint(t *testing.T) {
	d := geo.Haversine(40.7128, -74.0060, 40.7128, -74.0060)
	if d != 0 {
		t.Errorf("same point should be 0, got %f", d)
	}
}

func TestHaversine_KnownDistance(t *testing.T) {
	// NYC (40.7128, -74.0060) to London (51.5074, -0.1278) is ~5570 km.
	d := geo.Haversine(40.7128, -74.0060, 51.5074, -0.1278)
	const wantKm = 5570.0
	const toleranceKm = 10.0
	got := d / 1000.0
	if math.Abs(got-wantKm) > toleranceKm {
		t.Errorf("NYC→London: expected ~%f km, got %f km (diff %f)", wantKm, got, math.Abs(got-wantKm))
	}
}

func TestHaversine_ShortDistance(t *testing.T) {
	// Two points ~1 km apart in NYC.
	d := geo.Haversine(40.7128, -74.0060, 40.7200, -74.0000)
	const wantMeters = 940.0
	const toleranceMeters = 20.0
	if math.Abs(d-wantMeters) > toleranceMeters {
		t.Errorf("expected ~%f m, got %f m", wantMeters, d)
	}
}

func TestHaversine_Symmetry(t *testing.T) {
	a := geo.Haversine(40.7128, -74.0060, 48.8566, 2.3522)
	b := geo.Haversine(48.8566, 2.3522, 40.7128, -74.0060)
	if math.Abs(a-b) > 1.0 {
		t.Errorf("haversine should be symmetric: %f vs %f", a, b)
	}
}

func TestSoloFare(t *testing.T) {
	// 10km distance: 3.0 + 1.5 * 10 = 18.0
	fare := geo.SoloFare(10000)
	if math.Abs(fare-18.0) > 0.001 {
		t.Errorf("expected 18.0, got %f", fare)
	}
}

func TestPoolFare(t *testing.T) {
	// 1 rider: same as solo (18.0)
	f1 := geo.PoolFare(10000, 1)
	if math.Abs(f1-18.0) > 0.001 {
		t.Errorf("expected 18.0 for 1 rider, got %f", f1)
	}

	// 2 riders: (18.0 / 2) + 0.30 * 10 * 1 = 9.0 + 3.0 = 12.0
	f2 := geo.PoolFare(10000, 2)
	if math.Abs(f2-12.0) > 0.001 {
		t.Errorf("expected 12.0 for 2 riders, got %f", f2)
	}

	// Pool fare should be strictly less than solo fare for each passenger
	if f2 >= f1 {
		t.Errorf("pool fare (%f) should be less than solo fare (%f)", f2, f1)
	}
}

func TestSpatialCellID(t *testing.T) {
	// Points very close together should be in same cell
	c1 := geo.CellID(40.7128, -74.0060)
	c2 := geo.CellID(40.7130, -74.0061)
	if c1 != c2 {
		t.Errorf("expected nearby points in same cell: %s vs %s", c1, c2)
	}

	// Points 15km away should be in different cells
	c3 := geo.CellID(40.8500, -73.9000)
	if c1 == c3 {
		t.Errorf("distant points should have different cell IDs")
	}

	poly := geo.CellPolygon(c1)
	if len(poly) != 6 {
		t.Errorf("expected 6 hexagon vertices, got %d", len(poly))
	}
}

func TestSurgeEngine(t *testing.T) {
	engine := geo.NewSurgeEngine(0)
	lat, lng := 40.7128, -74.0060

	// Initially balanced: 1.0x
	if m := engine.GetMultiplier(lat, lng); m != 1.0 {
		t.Errorf("expected initial 1.0x, got %f", m)
	}

	// Simulate high demand spike: 8 ride requests in the same cell with 1 driver
	for i := 0; i < 8; i++ {
		engine.RecordDemand(lat, lng)
	}
	engine.UpdateSupply(geo.CellID(lat, lng), 1)

	surge := engine.GetMultiplier(lat, lng)
	if surge <= 1.0 {
		t.Errorf("expected surge > 1.0, got %f", surge)
	}
	if surge > 2.5 {
		t.Errorf("expected surge capped at 2.5, got %f", surge)
	}

	zones := engine.GetActiveZones()
	if len(zones) == 0 {
		t.Errorf("expected at least 1 active surge zone")
	}
	if zones[0].Multiplier != surge {
		t.Errorf("zone multiplier mismatch: %f vs %f", zones[0].Multiplier, surge)
	}
}
