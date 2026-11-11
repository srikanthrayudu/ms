package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/starfederation/datastar-go/datastar"

	"ms/api-gateway/internal/auth"
	driverv1 "ms/proto/gen/driver/v1"
	locationv1 "ms/proto/gen/location/v1"
	ridev1 "ms/proto/gen/ride/v1"
	"ms/shared/events"
)

type onboardSignals struct {
	License string `json:"license"`
	Plate   string `json:"plate"`
	Model   string `json:"model"`
}

// apiDriverOnboard registers driver vehicle details.
func (a *App) apiDriverOnboard(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	var s onboardSignals
	_ = datastar.ReadSignals(r, &s)

	_, err := a.clients.Driver.OnboardDriver(r.Context(), &driverv1.OnboardDriverRequest{
		UserId: user.ID, LicenseNumber: s.License, VehiclePlate: s.Plate, VehicleModel: s.Model,
	})
	sse := datastar.NewSSE(w, r)
	if err != nil {
		_ = sse.PatchElements(fmt.Sprintf(`<div id="form-error" class="error">%s</div>`, escapeHTML(err.Error())))
		return
	}
	_ = sse.Redirect("/driver")
}

type availSignals struct {
	Online bool `json:"online"`
}

// apiDriverAvailability toggles the driver's online status and (de)activates
// the simulated driving loop on the client.
func (a *App) apiDriverAvailability(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	var s availSignals
	_ = datastar.ReadSignals(r, &s)

	_, err := a.clients.Driver.SetAvailability(r.Context(), &driverv1.SetAvailabilityRequest{
		Id: user.ID, IsAvailable: s.Online,
	})
	sse := datastar.NewSSE(w, r)
	if err != nil {
		_ = sse.PatchElements(fmt.Sprintf(`<div id="driver-status" class="status error">%s</div>`, escapeHTML(err.Error())))
		return
	}
	if s.Online {
		_ = sse.PatchElements(`<div id="driver-status" class="status online">Online — waiting for ride requests…</div>`)
		_ = sse.ExecuteScript("window.startDrivingLoop()")
	} else {
		_ = sse.PatchElements(`<div id="driver-status" class="status offline">Offline</div>`)
		_ = sse.ExecuteScript("window.stopDrivingLoop()")
	}
}

type locSignals struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// apiDriverLocation records a driver GPS fix. Called by the driver page's
// simulated driving loop via a plain fetch (returns 204).
func (a *App) apiDriverLocation(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	var s locSignals
	_ = datastar.ReadSignals(r, &s)
	_, err := a.clients.Location.UpdateLocation(r.Context(), &locationv1.UpdateLocationRequest{
		DriverId: user.ID, Latitude: s.Lat, Longitude: s.Lng,
	})
	if err != nil {
		a.log.Warn("update driver location", "err", err, "driver", user.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// apiDriverStream pushes the driver's current/assigned ride and updates it live.
func (a *App) apiDriverStream(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	sse := datastar.NewSSE(w, r)

	if !a.natsOK() {
		_ = sse.PatchElements(`<div id="current-ride" class="status error">Realtime unavailable (NATS not connected).</div>`)
		return
	}

	// Push the driver's current active ride if one exists.
	if list, err := a.clients.Ride.ListMyRides(r.Context(), &ridev1.ListMyRidesRequest{
		UserId: user.ID, Role: "driver", Limit: 5,
	}); err == nil {
		for _, ride := range list.GetRides() {
			if isActiveRide(ride.GetStatus()) {
				sendDriverRide(sse, ride)
				break
			}
		}
	}

	sub, err := a.nc.Subscribe("rides.>", func(m *nats.Msg) {
		if sse.IsClosed() {
			return
		}
		var ev events.RideEvent
		if err := json.Unmarshal(m.Data, &ev); err != nil {
			return
		}
		ride, err := a.clients.Ride.GetRide(context.Background(), &ridev1.GetRideRequest{Id: ev.RideID})
		if err != nil {
			return
		}
		if ride.GetDriverId() != user.ID {
			return
		}
		sendDriverRide(sse, ride)
	})
	if err == nil {
		defer sub.Unsubscribe()
	}

	<-r.Context().Done()
}

// sendDriverRide pushes the current-ride panel and imperative map calls.
func sendDriverRide(sse *datastar.ServerSentEventGenerator, ride *ridev1.Ride) {
	_ = sse.PatchElements(renderDriverRidePanel(ride))
	if ride.GetPickup() != nil && ride.GetDropoff() != nil {
		_ = sse.ExecuteScript(fmt.Sprintf(
			"window.onRideMatched(%q,%f,%f,%f,%f)",
			ride.GetId(),
			ride.GetPickup().GetLatitude(), ride.GetPickup().GetLongitude(),
			ride.GetDropoff().GetLatitude(), ride.GetDropoff().GetLongitude(),
		))
	}
	_ = sse.ExecuteScript(fmt.Sprintf("window.onRideStatus(%q)", ride.GetStatus().String()))
}

// renderDriverRidePanel returns the #current-ride element with status-appropriate controls.
func renderDriverRidePanel(ride *ridev1.Ride) string {
	id := ride.GetId()
	status := ride.GetStatus().String()
	var controls string
	switch ride.GetStatus() {
	case ridev1.RideStatus_MATCHED:
		controls = fmt.Sprintf(`<button class="btn" data-on-click="@post('/api/rides/%s/start')">Start trip (on the way)</button>`, id)
	case ridev1.RideStatus_ON_THE_WAY:
		controls = fmt.Sprintf(`<button class="btn" data-on-click="@post('/api/rides/%s/pickup')">Pick up passenger</button>`, id)
	case ridev1.RideStatus_ONGOING:
		controls = fmt.Sprintf(`<button class="btn" data-on-click="@post('/api/rides/%s/complete')">Complete trip</button>`, id)
	case ridev1.RideStatus_COMPLETED:
		controls = fmt.Sprintf(`<div class="fare">Trip completed — fare $%.2f</div>`, ride.GetFare())
	case ridev1.RideStatus_CANCELLED:
		controls = `<div class="cancelled">Trip cancelled</div>`
	}
	return fmt.Sprintf(`<div id="current-ride" class="ride-card">
		<h3>Active ride <code>%s</code></h3>
		<div class="row"><span class="badge %s">%s</span></div>
		<div class="coords">Pickup: %.5f, %.5f</div>
		<div class="coords">Dropoff: %.5f, %.5f</div>
		<div class="controls">%s</div>
	</div>`, id, statusClass(status), status,
		ride.GetPickup().GetLatitude(), ride.GetPickup().GetLongitude(),
		ride.GetDropoff().GetLatitude(), ride.GetDropoff().GetLongitude(),
		controls)
}

func isActiveRide(s ridev1.RideStatus) bool {
	switch s {
	case ridev1.RideStatus_REQUESTED, ridev1.RideStatus_MATCHED,
		ridev1.RideStatus_ON_THE_WAY, ridev1.RideStatus_ONGOING:
		return true
	}
	return false
}
