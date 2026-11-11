package server

import (
	"net/http"

	driverv1 "ms/proto/gen/driver/v1"
	"ms/api-gateway/internal/auth"
)

func (a *App) pageIndex(w http.ResponseWriter, r *http.Request) {
	a.render(w, "index", PageData{User: auth.FromContext(r.Context())})
}

func (a *App) pageLogin(w http.ResponseWriter, r *http.Request) {
	if auth.FromContext(r.Context()) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	a.render(w, "login", PageData{})
}

func (a *App) pageRegister(w http.ResponseWriter, r *http.Request) {
	if auth.FromContext(r.Context()) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	a.render(w, "register", PageData{})
}

func (a *App) pageDashboard(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	a.render(w, "dashboard", PageData{User: u, UserName: a.userName(r.Context(), u)})
}

func (a *App) pageRide(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	a.render(w, "ride", PageData{User: u, UserName: a.userName(r.Context(), u), RideID: r.PathValue("id")})
}

// pageDriver renders the driver dashboard. If the driver is not yet onboarded
// it shows the onboarding form; otherwise it shows the availability + map view.
func (a *App) pageDriver(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	onboarded := false
	if u != nil {
		if _, err := a.clients.Driver.GetDriver(r.Context(), &driverv1.GetDriverRequest{Id: u.ID}); err == nil {
			onboarded = true
		}
	}
	a.render(w, "driver", PageData{User: u, UserName: a.userName(r.Context(), u), Onboarded: onboarded})
}
