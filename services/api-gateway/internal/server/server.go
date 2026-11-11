// Package server wires the gateway HTTP server, templates and middleware.
package server

import (
	"context"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/starfederation/datastar-go/datastar"

	"ms/api-gateway/internal/auth"
	"ms/api-gateway/internal/clients"
	"ms/api-gateway/internal/config"
	"ms/api-gateway/web"
	userv1 "ms/proto/gen/user/v1"
	mslog "ms/shared/log"
)

// DefaultLat/DefaultLng is the city centre used to seed all demo maps so that
// passenger pickup and driver location start close enough to match.
const (
	DefaultLat = 40.7128
	DefaultLng = -74.0060
)

// PageData is passed to every page template.
type PageData struct {
	Title      string
	User       *auth.User
	UserName   string
	RideID     string
	DefaultLat float64
	DefaultLng float64
	Onboarded  bool
}

// App is the gateway application.
type App struct {
	cfg     config.Config
	clients *clients.Clients
	nc      *nats.Conn
	log     *slog.Logger
	pages   map[string]*template.Template
}

// New builds the App, parsing all templates.
func New(cfg config.Config, cl *clients.Clients, nc *nats.Conn) (*App, error) {
	a := &App{
		cfg:     cfg,
		clients: cl,
		nc:      nc,
		log:     mslog.New("api-gateway", cfg.LogLevel),
	}
	if err := a.loadTemplates(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *App) loadTemplates() error {
	funcs := template.FuncMap{
		"roleLabel": func(role string) string {
			if role == "driver" {
				return "Driver"
			}
			return "Passenger"
		},
		"postSSE": datastar.PostSSE,
		"getSSE":  datastar.GetSSE,
	}
	base := template.Must(template.New("layout").Funcs(funcs).ParseFS(web.TemplatesFS, "templates/layout.html"))
	names := []string{"index", "login", "register", "dashboard", "ride", "driver", "payments"}
	a.pages = make(map[string]*template.Template, len(names))
	for _, n := range names {
		clone := template.Must(base.Clone())
		clone = template.Must(clone.ParseFS(web.TemplatesFS, "templates/"+n+".html"))
		a.pages[n] = clone
	}
	return nil
}

// render executes the named page template with the given data.
func (a *App) render(w http.ResponseWriter, page string, data PageData) {
	tmpl, ok := a.pages[page]
	if !ok {
		a.log.Error("unknown page template", "page", page)
		http.Error(w, "unknown page", http.StatusInternalServerError)
		return
	}
	if data.DefaultLat == 0 && data.DefaultLng == 0 {
		data.DefaultLat = DefaultLat
		data.DefaultLng = DefaultLng
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		a.log.Error("render template", "page", page, "err", err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// Routes builds the HTTP handler with all routes and middleware.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()

	staticFS, _ := fs.Sub(web.StaticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	// Pages
	mux.HandleFunc("GET /{$}", a.pageIndex)
	mux.HandleFunc("GET /login", a.pageLogin)
	mux.HandleFunc("GET /register", a.pageRegister)
	mux.HandleFunc("GET /dashboard", a.requireAuth(a.pageDashboard))
	mux.HandleFunc("GET /rides/{id}", a.requireAuth(a.pageRide))
	mux.HandleFunc("GET /driver", a.requireAuth(a.pageDriver))
	mux.HandleFunc("GET /payments", a.requireAuth(a.pagePayments))

	// Auth API
	mux.HandleFunc("POST /api/login", a.apiLogin)
	mux.HandleFunc("POST /api/register", a.apiRegister)
	mux.HandleFunc("POST /api/logout", a.apiLogout)

	// Ride API
	mux.HandleFunc("POST /api/rides", a.requireAuthAPI(a.apiRequestRide))
	mux.HandleFunc("GET /api/rides/{id}/stream", a.requireAuthAPI(a.apiRideStream))
	mux.HandleFunc("POST /api/rides/{id}/start", a.requireAuthAPI(a.apiRideStart))
	mux.HandleFunc("POST /api/rides/{id}/pickup", a.requireAuthAPI(a.apiRidePickup))
	mux.HandleFunc("POST /api/rides/{id}/complete", a.requireAuthAPI(a.apiRideComplete))

	// Driver API
	mux.HandleFunc("POST /api/driver/onboard", a.requireAuthAPI(a.apiDriverOnboard))
	mux.HandleFunc("POST /api/driver/availability", a.requireAuthAPI(a.apiDriverAvailability))
	mux.HandleFunc("POST /api/driver/location", a.requireAuthAPI(a.apiDriverLocation))
	mux.HandleFunc("GET /api/driver/stream", a.requireAuthAPI(a.apiDriverStream))

	// Payment API
	mux.HandleFunc("GET /api/payments", a.requireAuthAPI(a.apiListPayments))
	mux.HandleFunc("GET /api/payments/{rideId}", a.requireAuthAPI(a.apiGetPayment))

	// Notification API
	mux.HandleFunc("GET /api/notifications/stream", a.requireAuthAPI(a.apiNotificationStream))
	mux.HandleFunc("POST /api/notifications/read", a.requireAuthAPI(a.apiMarkNotificationsRead))

	return a.userMW(mux)
}

// userMW loads the user from the JWT cookie on every request.
func (a *App) userMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := auth.FromRequest(r, a.cfg.JWTSecret)
		next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), u)))
	})
}

// requireAuth redirects anonymous users to the login page.
func (a *App) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if auth.FromContext(r.Context()) == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		h(w, r)
	}
}

// requireAuthAPI rejects anonymous requests with 401.
func (a *App) requireAuthAPI(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if auth.FromContext(r.Context()) == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

// userName looks up a user's display name via the user-service.
func (a *App) userName(ctx context.Context, u *auth.User) string {
	if u == nil {
		return ""
	}
	p, err := a.clients.User.GetProfile(ctx, &userv1.GetProfileRequest{UserId: u.ID})
	if err != nil {
		return u.ID
	}
	return p.GetName()
}

// natsOK reports whether NATS is connected; streams use it to bail early.
func (a *App) natsOK() bool {
	return a.nc != nil && a.nc.Status() == nats.CONNECTED
}
