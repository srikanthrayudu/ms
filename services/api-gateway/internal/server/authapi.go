package server

import (
	"net/http"
	"time"

	"github.com/starfederation/datastar-go/datastar"

	userv1 "ms/proto/gen/user/v1"
)

type loginSignals struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type registerSignals struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Role     string `json:"role"`
}

func (a *App) apiLogin(w http.ResponseWriter, r *http.Request) {
	var s loginSignals
	_ = datastar.ReadSignals(r, &s)

	resp, err := a.clients.User.Login(r.Context(), &userv1.LoginRequest{
		Email: s.Email, Password: s.Password,
	})
	if err != nil {
		sse := datastar.NewSSE(w, r)
		_ = sse.PatchElements(`<div id="form-error" class="error">Invalid email or password.</div>`)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "token", Value: resp.GetToken(), Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int((24 * time.Hour).Seconds()),
	})
	sse := datastar.NewSSE(w, r)
	dest := "/dashboard"
	if resp.GetRole() == userv1.UserRole_DRIVER {
		dest = "/driver"
	}
	_ = sse.Redirect(dest)
}

func (a *App) apiRegister(w http.ResponseWriter, r *http.Request) {
	var s registerSignals
	_ = datastar.ReadSignals(r, &s)

	role := userv1.UserRole_PASSENGER
	if s.Role == "driver" {
		role = userv1.UserRole_DRIVER
	}

	if _, err := a.clients.User.Register(r.Context(), &userv1.RegisterRequest{
		Email: s.Email, Password: s.Password, Name: s.Name, Phone: s.Phone, Role: role,
	}); err != nil {
		sse := datastar.NewSSE(w, r)
		_ = sse.PatchElements(`<div id="form-error" class="error">Registration failed (email may already be in use).</div>`)
		return
	}

	// Auto-login so the user lands on the right dashboard.
	login, err := a.clients.User.Login(r.Context(), &userv1.LoginRequest{Email: s.Email, Password: s.Password})
	if err != nil {
		sse := datastar.NewSSE(w, r)
		_ = sse.Redirect("/login")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "token", Value: login.GetToken(), Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int((24 * time.Hour).Seconds()),
	})
	sse := datastar.NewSSE(w, r)
	dest := "/dashboard"
	if login.GetRole() == userv1.UserRole_DRIVER {
		dest = "/driver"
	}
	_ = sse.Redirect(dest)
}

func (a *App) apiLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "token", Value: "", Path: "/", MaxAge: -1})
	sse := datastar.NewSSE(w, r)
	_ = sse.Redirect("/")
}
