package gui

import (
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// guardForDeployment retains the original localhost-only security by default.
// NAS mode is enabled only when an explicit password-file path is configured.
func guardForDeployment(next http.Handler) http.Handler {
	passwordFile := strings.TrimSpace(os.Getenv("KINOPUB_REMOTE_PASSWORD_FILE"))
	if passwordFile == "" {
		return guardLocalOnly(next)
	}

	secret, err := os.ReadFile(passwordFile)
	if err != nil {
		panic("kinopub NAS: cannot read password file: " + err.Error())
	}
	password := strings.TrimSpace(string(secret))
	if len(password) < 24 {
		panic("kinopub NAS: password must contain at least 24 characters")
	}
	user := os.Getenv("KINOPUB_REMOTE_USER")
	if user == "" {
		user = "kinopub"
	}

	// An explicit Host allowlist also prevents DNS rebinding, even when the
	// NAS accepts incoming traffic on a non-loopback address.
	allowed := make(map[string]bool)
	for _, h := range strings.Split(os.Getenv("KINOPUB_REMOTE_ALLOWED_HOSTS"), ",") {
		h = strings.TrimSpace(h)
		if h != "" {
			allowed[h] = true
		}
	}
	if len(allowed) == 0 {
		panic("kinopub NAS: KINOPUB_REMOTE_ALLOWED_HOSTS is required")
	}
	wantedUser := sha256.Sum256([]byte(user))
	wantedPass := sha256.Sum256([]byte(password))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Docker healthcheck remains private to the container loopback.
		if r.Method == http.MethodGet && r.URL.Path == "/api/health" && isLoopbackRemote(r.RemoteAddr) {
			next.ServeHTTP(w, r)
			return
		}

		if !allowed[r.Host] {
			writeErr(w, http.StatusForbidden, "host is not allowed")
			return
		}
		actualUser, actualPass, ok := r.BasicAuth()
		uHash := sha256.Sum256([]byte(actualUser))
		pHash := sha256.Sum256([]byte(actualPass))
		if !ok || subtle.ConstantTimeCompare(uHash[:], wantedUser[:]) != 1 || subtle.ConstantTimeCompare(pHash[:], wantedPass[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="KinoPub NAS", charset="UTF-8"`)
			writeErr(w, http.StatusUnauthorized, "authentication required")
			return
		}

		// Cross-site requests must not inherit cached browser credentials.
		if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
			writeErr(w, http.StatusForbidden, "cross-site request denied")
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && !sameOriginHost(origin, r.Host) {
			writeErr(w, http.StatusForbidden, "foreign Origin denied")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			// Older browsers may omit Origin; Sec-Fetch-Site=same-origin
			// provides a safe alternative. Otherwise reject the mutation.
			if origin == "" && r.Header.Get("Sec-Fetch-Site") != "same-origin" {
				writeErr(w, http.StatusForbidden, "Origin required")
				return
			}
		}

		// Docker images must be replaced atomically, not self-modified over HTTP.
		if r.URL.Path == "/api/update/apply" || r.URL.Path == "/api/deps/install" {
			writeErr(w, http.StatusForbidden, "disabled in Docker builds")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackRemote(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func sameOriginHost(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u == nil || u.Host != host || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Path == "" || u.Path == "/"
}
