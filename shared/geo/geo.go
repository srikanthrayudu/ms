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
