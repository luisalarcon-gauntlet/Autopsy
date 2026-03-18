package server

import (
	"net/http"
	"time"

	"github.com/yourusername/autopsy/internal/auth"
)

const authCookieName = "autopsy_user"

// RequireAuth wraps a handler and enforces cookie-based authentication.
// /login and /healthz are exempt. Valid cookies attach the User to the request
// context; missing or unknown cookies redirect to /login.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(authCookieName)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		user, ok := auth.Users[c.Value]
		if !ok {
			clearAuthCookie(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ctx := auth.WithUser(r.Context(), user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// HandleLoginPage serves GET /login.
func (h *Handler) HandleLoginPage(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(authCookieName); err == nil {
		if _, ok := auth.Users[c.Value]; ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}
	if err := h.loginTmpl.ExecuteTemplate(w, "login.html", nil); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// HandleLoginPost processes POST /login, sets the session cookie, redirects to /.
func (h *Handler) HandleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	username := r.FormValue("username")
	if _, ok := auth.Users[username]; !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    username,
		Path:     "/",
		MaxAge:   7 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// HandleLogout clears the auth cookie and redirects to /login.
func (h *Handler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	clearAuthCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func clearAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:    authCookieName,
		Value:   "",
		Path:    "/",
		MaxAge:  -1,
		Expires: time.Unix(0, 0),
	})
}
