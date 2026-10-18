package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNASGuard(t *testing.T) {
	const password = "TEST_PASSWORD_GENERATED_FOR_TESTING_12345"
	file := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(file, []byte(password), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KINOPUB_REMOTE_PASSWORD_FILE", file)
	t.Setenv("KINOPUB_REMOTE_USER", "kinopub")
	t.Setenv("KINOPUB_REMOTE_ALLOWED_HOSTS", "192.168.2.10:8765")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := guardForDeployment(next)
	request := func(method, path, host, pass, origin, site, remote string) int {
		r := httptest.NewRequest(method, "http://"+host+path, nil)
		r.Host = host
		r.RemoteAddr = remote
		if pass != "" {
			r.SetBasicAuth("kinopub", pass)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	type tc struct {
		name, method, path, host, password, origin, site, remote string
		want                                                     int
	}
	for _, tt := range []tc{
		{"anonymous", "GET", "/", "192.168.2.10:8765", "", "", "", "192.168.1.20:1234", 401},
		{"wrong-password", "GET", "/", "192.168.2.10:8765", "incorrect", "", "", "192.168.1.20:1234", 401},
		{"authenticated-get", "GET", "/", "192.168.2.10:8765", password, "", "", "192.168.1.20:1234", 204},
		{"host-rebinding", "GET", "/", "evil.example:8765", password, "", "", "192.168.1.20:1234", 403},
		{"post-with-origin", "POST", "/api/jobs", "192.168.2.10:8765", password, "http://192.168.2.10:8765", "same-origin", "192.168.1.20:1234", 204},
		{"post-with-fetch", "POST", "/api/jobs", "192.168.2.10:8765", password, "", "same-origin", "192.168.1.20:1234", 204},
		{"post-without-origin", "POST", "/api/jobs", "192.168.2.10:8765", password, "", "", "192.168.1.20:1234", 403},
		{"foreign-origin", "POST", "/api/jobs", "192.168.2.10:8765", password, "https://evil.example", "same-origin", "192.168.1.20:1234", 403},
		{"cross-site", "GET", "/", "192.168.2.10:8765", password, "", "cross-site", "192.168.1.20:1234", 403},
		{"disabled-update", "POST", "/api/update/apply", "192.168.2.10:8765", password, "http://192.168.2.10:8765", "same-origin", "192.168.1.20:1234", 403},
		{"disabled-installer", "POST", "/api/deps/install", "192.168.2.10:8765", password, "http://192.168.2.10:8765", "same-origin", "192.168.1.20:1234", 403},
		{"remote-health", "GET", "/api/health", "192.168.2.10:8765", "", "", "", "192.168.1.20:1234", 401},
		{"local-health", "GET", "/api/health", "127.0.0.1:8765", "", "", "", "127.0.0.1:1234", 204},
		{"fake-local-health", "GET", "/api/health", "127.0.0.1:8765", "", "", "", "192.168.1.20:1234", 403},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := request(tt.method, tt.path, tt.host, tt.password, tt.origin, tt.site, tt.remote)
			if got != tt.want {
				t.Fatalf("HTTP %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNASGuardDefaultsToLocalhostOnly(t *testing.T) {
	t.Setenv("KINOPUB_REMOTE_PASSWORD_FILE", "")
	h := guardForDeployment(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	r := httptest.NewRequest("GET", "http://192.168.2.10:8765/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("HTTP %d, want localhost restriction", w.Code)
	}
}

func TestNASGuardRejectsShortSecret(t *testing.T) {
	file := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(file, []byte("weak"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KINOPUB_REMOTE_PASSWORD_FILE", file)
	t.Setenv("KINOPUB_REMOTE_ALLOWED_HOSTS", "192.168.2.10:8765")
	defer func() {
		if recover() == nil {
			t.Fatal("short password must fail startup")
		}
	}()
	_ = guardForDeployment(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
}
