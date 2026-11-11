// Package events defines the NATS subjects and event payloads shared
// between services and the API gateway.
//
// Subject layout:
//   rides.<rideID>.<eventType>      ride lifecycle events (matched/started/...)
//   drivers.<driverID>.location      a driver's latest GPS fix
package events

import "time"

// Ride lifecycle event types.
const (
	RideMatched   = "matched"
	RideStarted   = "started"   // driver is on the way to pickup
	RideOngoing   = "ongoing"   // trip in progress
	RideCompleted = "completed"
	RideCancelled = "cancelled"
)

// Subjects
const (
	RideEventsPrefix   = "rides."
	DriverLocationPrefix = "drivers."
)

// RideSubject builds a per-ride event subject: rides/<rideID>/<eventType>.
func RideSubject(rideID, eventType string) string {
	return RideEventsPrefix + rideID + "." + eventType
}

// RideSubjectAll returns the wildcard subscription for all events of a ride.
func RideSubjectAll(rideID string) string {
	return RideEventsPrefix + rideID + ".*"
}

// DriverLocationSubject builds a driver location subject: drivers/<driverID>/location.
func DriverLocationSubject(driverID string) string {
	return DriverLocationPrefix + driverID + ".location"
}

// RideEvent is published on every ride lifecycle transition.
type RideEvent struct {
	RideID      string  `json:"ride_id"`
	Type        string  `json:"type"`
	Status      string  `json:"status"`
	DriverID    string  `json:"driver_id,omitempty"`
	PassengerID string  `json:"passenger_id,omitempty"`
	Latitude    float64 `json:"latitude,omitempty"`
	Longitude   float64 `json:"longitude,omitempty"`
	Heading     float64 `json:"heading,omitempty"`
	Fare        float64 `json:"fare,omitempty"`
	Timestamp   int64   `json:"timestamp"`
}

// DriverLocationEvent is published whenever a driver reports a new fix.
type DriverLocationEvent struct {
	DriverID  string  `json:"driver_id"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Heading   float64 `json:"heading"`
	Speed     float64 `json:"speed"`
	Timestamp int64   `json:"timestamp"`
}

// Now returns the current unix timestamp in seconds.
func Now() int64 { return time.Now().Unix() }
