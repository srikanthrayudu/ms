// Package geo provides geographic helpers used by the location and ride services.
package geo

import "math"

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

// PoolFare computes the per-rider fare for a pool trip.
// Each additional rider reduces everyone's cost; a 20% detour premium is added
// per extra rider to compensate the driver for the longer route.
// Formula: soloFare / riders  +  $0.30/km per extra rider (detour surcharge).
func PoolFare(distanceMeters float64, riders int) float64 {
	if riders <= 0 {
		riders = 1
	}
	solo := SoloFare(distanceMeters)
	shared := solo / float64(riders)
	detourPerRider := 0.30 * (distanceMeters / 1000.0) * float64(riders-1)
	return shared + detourPerRider
}
