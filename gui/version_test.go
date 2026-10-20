package gui_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/gui"
	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
)

// newServerAt is newServer at a given version, refusing clients below API
// level 2. The client starts at level 2; tests lower it to be refused.
func newServerAt(t *testing.T, version string) *client.Client {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s, err := store.NewSQLite(filepath.Join(t.TempDir(), "gui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h, err := handlers.NewHandler(s, false, fstest.MapFS{}, "test", version)
	if err != nil {
		t.Fatal(err)
	}
	r, err := router.NewRouter(router.Config{
		Handler: h, LogSink: discardSink{}, Settings: types.AppSettings{AuthToken: "tok"},
		Assets: fstest.MapFS{}, Version: version, APILevel: 2, MinClientAPI: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	c := client.New(srv.URL, "tok")
	c.APILevel = 2
	return c
}

// counter counts the requests that reach the server.
type counter struct{ n atomic.Int64 }

func (c *counter) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return http.DefaultTransport.RoundTrip(r)
}

func setGUIVersion(t *testing.T, v string) {
	old := gui.Version
	gui.Version = v
	t.Cleanup(func() { gui.Version = old })
}

func TestTooOldAppGoesReadOnlyAndKeepsQueuedEdits(t *testing.T) {
	c := newServerAt(t, "v0.1.0")
	create(t, c, "# Groceries\nmilk")
	cb := plug(c)
	path := filepath.Join(t.TempDir(), "cache.json")
	ui, _ := openCached(t, c, path)

	// An edit made while the server is down waits in the queue.
	ui.List.Select(0)
	ui.EditToggle.OnTapped()
	ui.Editor.SetText("# Groceries\nmilk and eggs")
	cb.unplugged.Store(true)
	ui.Save.OnTapped()

	// Back online, the server now refuses this version.
	cb.unplugged.Store(false)
	c.APILevel = 1
	ui.Sync()

	if ui.ConnLabel.Text != "Update needed" {
		t.Errorf("indicator = %q, want Update needed", ui.ConnLabel.Text)
	}
	if ui.ConnDot.FillColor != theme.WarningColor() {
		t.Errorf("indicator color = %v, want the warning color", ui.ConnDot.FillColor)
	}
	if want := "This version is too old for the server. Download the latest release."; !ui.Upgrade.Visible() || ui.UpgradeLabel.Text != want {
		t.Errorf("update message visible=%v %q, want %q", ui.Upgrade.Visible(), ui.UpgradeLabel.Text, want)
	}
	var opened *url.URL
	restore := gui.SetOpenURLForTest(func(u *url.URL) error { opened = u; return nil })
	defer restore()
	ui.UpgradeButton.OnTapped()
	if opened == nil || opened.String() != client.ReleasesURL {
		t.Errorf("button opened %v, want %s", opened, client.ReleasesURL)
	}

	// The edit stays queued on disk and is never sent.
	if e, ok := gui.PendingForTest(ui, 1); !ok || e.Err != nil || e.Body != "# Groceries\nmilk and eggs" {
		t.Fatalf("pending %+v %v, want the edit still queued", e, ok)
	}
	current := client.New(c.BaseURL, "tok")
	current.APILevel = 2
	if n, err := current.Get(context.Background(), 1); err != nil || n.Body != "# Groceries\nmilk" {
		t.Fatalf("server has %+v, %v; want the edit not sent", n, err)
	}

	// Syncing has stopped: neither a sync nor a click on the indicator
	// reaches the server.
	n := &counter{}
	c.HTTP.Transport = n
	ui.Sync()
	test.Tap(ui.ConnStatus)
	if n.n.Load() != 0 {
		t.Errorf("%d requests after the refusal, want none", n.n.Load())
	}
	if ui.ConnLabel.Text != "Update needed" {
		t.Errorf("indicator = %q after a click, want Update needed", ui.ConnLabel.Text)
	}

	// Still readable from the local copy, but not writable.
	assertTitles(t, ui, "Groceries")
	for name, b := range map[string]interface{ Disabled() bool }{
		"New": ui.NewNote, "Edit": ui.EditToggle, "Rename": ui.Rename, "Delete": ui.Delete,
	} {
		if !b.Disabled() {
			t.Errorf("%s is enabled in a too-old app", name)
		}
	}
	ui.Search.SetText("eggs")
	assertTitles(t, ui, "Groceries")
}

func TestNewerServerShowsTheReleaseOnce(t *testing.T) {
	setGUIVersion(t, "v0.1.0")
	c := newServerAt(t, "v0.2.0")
	ui, _ := open(t, c)

	if !ui.NewRelease.Visible() || ui.NewRelease.Text != "Scribble v0.2.0 is available" {
		t.Fatalf("release link visible=%v %q", ui.NewRelease.Visible(), ui.NewRelease.Text)
	}
	if want := "https://github.com/johansundell/scribble/releases/tag/v0.2.0"; ui.NewRelease.URL.String() != want {
		t.Errorf("link opens %s, want %s", ui.NewRelease.URL, want)
	}
	// Later syncs leave it alone.
	ui.NewRelease.SetText("seen")
	ui.Sync()
	if ui.NewRelease.Text != "seen" || !ui.NewRelease.Visible() {
		t.Errorf("the notice changed on a later sync: %q", ui.NewRelease.Text)
	}
	if !strings.Contains(gui.AboutTextForTest(ui), "Scribble v0.2.0 is available") {
		t.Errorf("About lacks the release: %q", gui.AboutTextForTest(ui))
	}
}

func TestNoReleaseNoticeForDevOrSameVersion(t *testing.T) {
	for _, own := range []string{"dev", "v0.2.0"} {
		t.Run(own, func(t *testing.T) {
			setGUIVersion(t, own)
			c := newServerAt(t, "v0.2.0")
			ui, _ := open(t, c)
			if ui.NewRelease.Visible() {
				t.Errorf("release notice shown for %s: %q", own, ui.NewRelease.Text)
			}
			if strings.Contains(gui.AboutTextForTest(ui), "is available") {
				t.Errorf("About shows a release for %s", own)
			}
		})
	}
}
