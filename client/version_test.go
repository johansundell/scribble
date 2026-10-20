package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/johansundell/scribble/client"
)

func TestClientSaysWhoItIs(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("X-Scribble-Client"))
		w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)

	c := client.New(srv.URL, "tok")
	c.Name, c.Version = "cli", "v0.0.9"
	c.List(context.Background(), "")
	c.Name, c.Version, c.APILevel = "gui", "", 7
	c.List(context.Background(), "")

	want := []string{"cli/1 v0.0.9", "gui/7 dev"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("X-Scribble-Client = %q, want %q", got, want)
	}
}

func TestIsUpgradeRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "This Scribble client is too old for this server.", http.StatusUpgradeRequired)
	}))
	t.Cleanup(srv.Close)

	_, err := client.New(srv.URL, "tok").List(context.Background(), "")
	if !client.IsUpgradeRequired(err) {
		t.Errorf("want upgrade-required, got %v", err)
	}
	if client.IsUpgradeRequired(&client.Error{Status: http.StatusNotFound}) {
		t.Error("a 404 is not an upgrade")
	}
}

// TestServerVersion checks that the client remembers the server's version
// and API level from any answer, refusals included.
func TestServerVersion(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Version", "v0.3.0")
		w.Header().Set("X-Scribble-API", "3")
		w.WriteHeader(status)
		w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)

	c := client.New(srv.URL, "tok")
	if v, api := c.ServerVersion(); v != "" || api != 0 {
		t.Errorf("before any request: %q %d", v, api)
	}
	status = http.StatusUpgradeRequired
	c.List(context.Background(), "")
	if v, api := c.ServerVersion(); v != "v0.3.0" || api != 3 {
		t.Errorf("ServerVersion = %q %d, want v0.3.0 3", v, api)
	}
}

func TestHealth(t *testing.T) {
	c := newClient(t)
	c.APILevel = 1
	h, err := c.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Version != "dev" || h.API != 1 {
		t.Errorf("Health = %+v, want version dev, API 1", h)
	}
}

func TestNewerRelease(t *testing.T) {
	cases := []struct {
		own, server string
		want        bool
	}{
		{"v0.0.9", "v0.0.10", true},
		{"v0.0.9", "v0.1.0", true},
		{"v0.9.9", "v1.0.0", true},
		{"0.0.9", "v0.0.10", true},
		{"v0.0.10", "v0.0.9", false},
		{"v0.0.9", "v0.0.9", false},
		{"dev", "v9.9.9", false},
		{"v0.0.9", "dev", false},
		{"", "v0.0.10", false},
		{"v0.0.9", "", false},
		{"v0.0.9", "v0.0", false},
		{"v0.0.9", "vx.y.z", false},
		{"v0.0.9-3-gabc", "v0.0.10", true},
		{"v0.0.9", "v0.0.10-rc1", true},
	}
	for _, tc := range cases {
		if got := client.NewerRelease(tc.own, tc.server); got != tc.want {
			t.Errorf("NewerRelease(%q, %q) = %v, want %v", tc.own, tc.server, got, tc.want)
		}
	}
}
