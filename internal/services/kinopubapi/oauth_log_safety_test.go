package kinopubapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOAuthLogsExcludeCredentials(t *testing.T) {
	const access = "SYNTHETIC_ACCESS_TOKEN_ABC123"
	const refresh = "SYNTHETIC_REFRESH_TOKEN_ABC123"
	const deviceCode = "SYNTHETIC_DEVICE_CODE_ABC123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("grant_type") {
		case "device_code":
			fmt.Fprintf(w, `{"code":%q,"user_code":"SAFE_CODE"}`, deviceCode)
		case "device_token", "refresh_token":
			fmt.Fprintf(w, `{"access_token":%q,"refresh_token":%q,"expires_in":86400}`, access, refresh)
		default:
			http.Error(w, "invalid test operation", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	var logs []string
	c := New(srv.Client(), Tokens{}, WithDebug(func(m string) { logs = append(logs, m) }))
	c.host = srv.URL

	if _, err := c.RequestDeviceCode(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PollDeviceToken(context.Background(), deviceCode); err != nil {
		t.Fatal(err)
	}
	if err := c.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(logs, "\n")
	for _, sensitive := range []string{access, refresh, deviceCode, "access_token", "\"refresh_token\""} {
		if strings.Contains(all, sensitive) {
			t.Fatal("OAuth debug log leaked response body or credentials")
		}
	}
	for _, marker := range []string{"grant_type=device_code", "grant_type=device_token", "grant_type=refresh_token", "HTTP 200"} {
		if !strings.Contains(all, marker) {
			t.Fatalf("missing safe OAuth status marker %q", marker)
		}
	}
}

func TestMalformedOAuthResponseDoesNotExposeBody(t *testing.T) {
	const sensitive = "SYNTHETIC_SECRET_IN_MALFORMED_BODY"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, `{"access_token":"`+sensitive+`"`)
	}))
	defer srv.Close()
	var logged string
	c := New(srv.Client(), Tokens{}, WithDebug(func(m string) { logged = m }))
	c.host = srv.URL
	_, err := c.RequestDeviceCode(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("unexpected error %v", err)
	}
	if strings.Contains(err.Error(), sensitive) || strings.Contains(logged, sensitive) {
		t.Fatal("response body leaked via diagnostic or error")
	}
}

type failingOAuthTransport struct{}

func (failingOAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, &url.Error{Op: r.Method, URL: r.URL.String(), Err: errors.New("synthetic network failure")}
}

func TestOAuthTransportErrorsDoNotExposeQueryTokens(t *testing.T) {
	const refresh = "SYNTHETIC_REFRESH_TOKEN_QUERY"
	client := &http.Client{Transport: failingOAuthTransport{}}
	c := New(client, Tokens{RefreshToken: refresh})
	c.host = "https://api.example.invalid"
	err := c.refresh(context.Background())
	if err == nil {
		t.Fatal("expected network error")
	}
	for _, secret := range []string{refresh, DefaultClientSecret, "client_secret="} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("transport error leaked URL query credentials")
		}
	}
}

func TestAPIFetchTransportErrorsDoNotExposeQueryTokens(t *testing.T) {
	const access = "SYNTHETIC_ACCESS_TOKEN_QUERY"
	c := New(&http.Client{Transport: failingOAuthTransport{}}, Tokens{AccessToken: access})
	c.host = "https://api.example.invalid"
	var out map[string]any
	err := c.get(context.Background(), "/items", nil, &out)
	if err == nil {
		t.Fatal("expected network error")
	}
	if strings.Contains(err.Error(), access) || strings.Contains(err.Error(), "access_token=") {
		t.Fatal("API transport error leaked access_token query")
	}
}
