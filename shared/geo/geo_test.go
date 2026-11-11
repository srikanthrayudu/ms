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
