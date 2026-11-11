package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/starfederation/datastar-go/datastar"

	"ms/api-gateway/internal/auth"
	locationv1 "ms/proto/gen/location/v1"
	ridev1 "ms/proto/gen/ride/v1"
	"ms/shared/events"
)

type rideRequestSignals struct {
	PickupLat  float64 `json:"pickupLat"`
	PickupLng  float64 `json:"pickupLng"`
	DropoffLat float64 `json:"dropoffLat"`
	DropoffLng float64 `json:"dropoffLng"`
}

// apiRequestRide creates a ride and redirects to its tracking page.
func (a *App) apiRequestRide(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	var s rideRequestSignals
	_ = datastar.ReadSignals(r, &s)

	ride, err := a.clients.Ride.RequestRide(r.Context(), &ridev1.RequestRideRequest{
		PassengerId: user.ID,
		Pickup:      &ridev1.GeoPoint{Latitude: s.PickupLat, Longitude: s.PickupLng},
		Dropoff:     &ridev1.GeoPoint{Latitude: s.DropoffLat, Longitude: s.DropoffLng},
	})
	sse := datastar.NewSSE(w, r)
	if err != nil {
		_ = sse.PatchElements(fmt.Sprintf(`<div id="form-error" class="error">%s</div>`, escapeHTML(err.Error())))
		return
	}
	_ = sse.Redirect("/rides/" + ride.GetId())
}

// apiRideStream streams live ride state to the passenger tracking page.
func (a *App) apiRideStream(w http.ResponseWriter, r *http.Request) {
	rideID := r.PathValue("id")
	sse := datastar.NewSSE(w, r)

	if !a.natsOK() {
		_ = sse.PatchElements(`<div id="ride-status" class="status error">Realtime unavailable (NATS not connected).</div>`)
		return
	}

	ride, err := a.clients.Ride.GetRide(r.Context(), &ridev1.GetRideRequest{Id: rideID})
	if err != nil {
		_ = sse.PatchElements(`<div id="ride-status" class="status error">Ride not found.</div>`)
		return
	}
	a.patchRideUI(sse, ride)

	var mu sync.Mutex
	var subs []*nats.Subscription
	subbedDrivers := make(map[string]bool)

	subDriver := func(driverID string) {
		mu.Lock()
		if driverID == "" || subbedDrivers[driverID] {
			mu.Unlock()
			return
		}
		subbedDrivers[driverID] = true
		mu.Unlock()
		s, err := a.nc.Subscribe(events.DriverLocationSubject(driverID), func(m *nats.Msg) {
			if sse.IsClosed() {
				return
			}
			var ev events.DriverLocationEvent
			if err := json.Unmarshal(m.Data, &ev); err != nil {
				return
			}
			_ = sse.ExecuteScript(fmt.Sprintf("window.moveDriverMarker(%f,%f)", ev.Latitude, ev.Longitude))
		})
		if err == nil {
			mu.Lock()
			subs = append(subs, s)
			mu.Unlock()
		}
	}

	rideSub, err := a.nc.Subscribe(events.RideSubjectAll(rideID), func(m *nats.Msg) {
		if sse.IsClosed() {
			return
		}
		var ev events.RideEvent
		if err := json.Unmarshal(m.Data, &ev); err != nil {
			return
		}
		if fresh, err := a.clients.Ride.GetRide(context.Background(), &ridev1.GetRideRequest{Id: ev.RideID}); err == nil {
			a.patchRideUI(sse, fresh)
		}
		if ev.Type == events.RideMatched && ev.DriverID != "" {
			subDriver(ev.DriverID)
		}
	})
	if err == nil {
		subs = append(subs, rideSub)
	}

	// If the ride is already matched, subscribe to that driver's location now.
	if ride.GetDriverId() != "" {
		subDriver(ride.GetDriverId())
		if loc, err := a.clients.Location.GetLocation(context.Background(), &locationv1.GetLocationRequest{DriverId: ride.GetDriverId()}); err == nil {
			_ = sse.ExecuteScript(fmt.Sprintf("window.moveDriverMarker(%f,%f)", loc.GetLatitude(), loc.GetLongitude()))
		}
	}

	<-r.Context().Done()
	for _, s := range subs {
		_ = s.Unsubscribe()
	}
}

// patchRideUI pushes the current ride state: a status panel, map markers and signals.
func (a *App) patchRideUI(sse *datastar.ServerSentEventGenerator, ride *ridev1.Ride) {
	_ = sse.PatchElements(renderRideStatusPanel(ride))
	if ride.GetPickup() != nil {
		_ = sse.ExecuteScript(fmt.Sprintf("window.placePickup(%f,%f)", ride.GetPickup().GetLatitude(), ride.GetPickup().GetLongitude()))
	}
	if ride.GetDropoff() != nil {
		_ = sse.ExecuteScript(fmt.Sprintf("window.placeDropoff(%f,%f)", ride.GetDropoff().GetLatitude(), ride.GetDropoff().GetLongitude()))
	}
	_ = sse.MarshalAndPatchSignals(map[string]any{
		"status":   ride.GetStatus().String(),
		"driverId": ride.GetDriverId(),
		"fare":     ride.GetFare(),
	})
}

// renderRideStatusPanel returns the HTML for the #ride-status element.
func renderRideStatusPanel(ride *ridev1.Ride) string {
	status := ride.GetStatus().String()
	driver := "Searching for a nearby driver…"
	if ride.GetDriverId() != "" {
		driver = "Driver assigned: " + ride.GetDriverId()
	}
	fare := ""
	if ride.GetStatus() == ridev1.RideStatus_COMPLETED {
		fare = fmt.Sprintf(`<div class="fare">Fare: $%.2f</div>`, ride.GetFare())
	}
	return fmt.Sprintf(`<div id="ride-status" class="status %s">
		<span class="badge %s">%s</span>
		<div class="driver">%s</div>
		%s
	</div>`, statusClass(status), statusClass(status), status, driver, fare)
}

func statusClass(status string) string {
	switch status {
	case "COMPLETED":
		return "done"
	case "CANCELLED":
		return "cancelled"
	case "ONGOING":
		return "active"
	case "ON_THE_WAY":
		return "coming"
	case "MATCHED":
		return "matched"
	default:
		return "pending"
	}
}

// apiRideStart advances a ride to ON_THE_WAY.
func (a *App) apiRideStart(w http.ResponseWriter, r *http.Request) {
	a.updateRideStatus(w, r, ridev1.RideStatus_ON_THE_WAY)
}

// apiRidePickup advances a ride to ONGOING.
func (a *App) apiRidePickup(w http.ResponseWriter, r *http.Request) {
	a.updateRideStatus(w, r, ridev1.RideStatus_ONGOING)
}

// apiRideComplete advances a ride to COMPLETED.
func (a *App) apiRideComplete(w http.ResponseWriter, r *http.Request) {
	a.updateRideStatus(w, r, ridev1.RideStatus_COMPLETED)
}

func (a *App) updateRideStatus(w http.ResponseWriter, r *http.Request, status ridev1.RideStatus) {
	user := auth.FromContext(r.Context())
	_, err := a.clients.Ride.UpdateRideStatus(r.Context(), &ridev1.UpdateRideStatusRequest{
		Id: r.PathValue("id"), Status: status, ActorId: user.ID,
	})
	sse := datastar.NewSSE(w, r)
	if err != nil {
		_ = sse.ConsoleError(err)
		return
	}
	_ = sse.ConsoleLog("ok")
}

// escapeHTML does minimal escaping of error text for safe inclusion in HTML.
func escapeHTML(s string) string {
	var b []byte
	for _, c := range s {
		switch c {
		case '<':
			b = append(b, '&', 'l', 't', ';')
		case '>':
			b = append(b, '&', 'g', 't', ';')
		case '&':
			b = append(b, '&', 'a', 'm', 'p', ';')
		case '"':
			b = append(b, '&', 'q', 'u', 'o', 't', ';')
		case '\'':
			b = append(b, '&', '#', '3', '9', ';')
		default:
			b = append(b, byte(c))
		}
	}
	return string(b)
}
