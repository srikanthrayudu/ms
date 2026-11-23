package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/starfederation/datastar-go/datastar"

	"ms/api-gateway/internal/auth"
	ridev1 "ms/proto/gen/ride/v1"
	"ms/shared/events"
)

// pageOps renders the Operations Control Room dispatch overview.
func (a *App) pageOps(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	a.render(w, "ops", PageData{
		Title:    "Operations Control Center — RideShare Fleet",
		User:     u,
		UserName: a.userName(r.Context(), u),
	})
}

// apiGetSurge returns current dynamic surge zones as JSON.
func (a *App) apiGetSurge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp, err := a.clients.Ride.GetSurgeZones(r.Context(), &ridev1.GetSurgeZonesRequest{})
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"zones": []any{}})
		return
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// apiOpsStream streams live fleet positions and ride events to the Operations dashboard.
func (a *App) apiOpsStream(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	if !a.natsOK() {
		_ = sse.PatchElements(`<div id="ops-alert" class="error">NATS event broker disconnected.</div>`)
		return
	}

	// 1. Subscribe to all driver location updates
	locSub, err := a.nc.Subscribe("drivers.*.location", func(m *nats.Msg) {
		if sse.IsClosed() {
			return
		}
		var ev events.DriverLocationEvent
		if err := json.Unmarshal(m.Data, &ev); err != nil {
			return
		}
		_ = sse.ExecuteScript(fmt.Sprintf(
			"window.updateFleetMarker(%q, %f, %f, true)",
			ev.DriverID, ev.Latitude, ev.Longitude,
		))
	})
	if err == nil {
		defer locSub.Unsubscribe()
	}

	// 2. Subscribe to all ride lifecycle events
	rideSub, err := a.nc.Subscribe("rides.>", func(m *nats.Msg) {
		if sse.IsClosed() {
			return
		}
		var ev events.RideEvent
		if err := json.Unmarshal(m.Data, &ev); err != nil {
			return
		}
		// Dispatch event to client via JavaScript hook
		_ = sse.ExecuteScript(fmt.Sprintf(
			"if (window.appendOpsEvent) window.appendOpsEvent(%q, %q, %q, %q)",
			ev.Type, ev.RideID, ev.Status, statusClass(ev.Status),
		))
	})
	if err == nil {
		defer rideSub.Unsubscribe()
	}

	<-r.Context().Done()
}
