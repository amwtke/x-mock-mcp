package mcptransport

import (
	"context"
	"crypto/subtle"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func HTTP(server *sdk.Server, token, host string) http.Handler {
	inner := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 4 << 20})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := "Bearer " + token
		if token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(expected)) != 1 {
			http.Error(w, "bearer token required", http.StatusUnauthorized)
			return
		}
		actualHost := r.Host
		hostname := actualHost
		if h, _, err := net.SplitHostPort(actualHost); err == nil {
			hostname = h
		}
		ip := net.ParseIP(hostname)
		if (host != "" && actualHost != host) || (hostname != "localhost" && (ip == nil || !ip.IsLoopback())) {
			http.Error(w, "unexpected Host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != actualHost || u.Scheme != "http" {
				http.Error(w, "unexpected Origin", http.StatusForbidden)
				return
			}
		}
		inner.ServeHTTP(w, r)
	})
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header = r.Header.Clone()
	copy.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(copy)
}
func Connect(ctx context.Context, endpoint, token string) (*sdk.ClientSession, error) {
	client := sdk.NewClient(&sdk.Implementation{Name: "x-mock-control", Version: "0.1.0"}, nil)
	return client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: strings.TrimRight(endpoint, "/"), HTTPClient: &http.Client{Transport: bearerTransport{token: token, base: http.DefaultTransport}}, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
}
