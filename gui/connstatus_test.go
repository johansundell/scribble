package gui_test

import (
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"github.com/johansundell/scribble/gui"
	"github.com/johansundell/scribble/offline"
)

func TestConnectionStatusInitialAndConnected(t *testing.T) {
	c := newServer(t)
	test.NewTempApp(t)
	w := test.NewWindow(nil)
	t.Cleanup(func() {
		if slices.Contains(fyne.CurrentApp().Driver().AllWindows(), w) {
			w.Close()
		}
	})
	cache, err := offline.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}

	ui := gui.New(w, c, cache)

	// Before any sync runs, indicator should show "Connecting..." with disabled/grey color.
	if ui.ConnLabel == nil || ui.ConnDot == nil || ui.ConnStatus == nil {
		t.Fatal("connection status widgets must be exported on UI")
	}
	if ui.ConnLabel.Text != "Connecting..." {
		t.Errorf("initial label = %q, want %q", ui.ConnLabel.Text, "Connecting...")
	}
	if ui.ConnDot.FillColor != theme.DisabledColor() {
		t.Errorf("initial dot color = %v, want %v", ui.ConnDot.FillColor, theme.DisabledColor())
	}

	// Once sync succeeds, indicator should update to "Connected" with success/green color.
	ui.Sync()
	if ui.ConnLabel.Text != "Connected" {
		t.Errorf("synced label = %q, want %q", ui.ConnLabel.Text, "Connected")
	}
	if ui.ConnDot.FillColor != theme.SuccessColor() {
		t.Errorf("synced dot color = %v, want %v", ui.ConnDot.FillColor, theme.SuccessColor())
	}
}

func TestConnectionStatusOfflineSync(t *testing.T) {
	c := newServer(t)
	cb := plug(c)
	test.NewTempApp(t)
	w := test.NewWindow(nil)
	t.Cleanup(func() {
		if slices.Contains(fyne.CurrentApp().Driver().AllWindows(), w) {
			w.Close()
		}
	})
	cache, err := offline.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}

	ui := gui.New(w, c, cache)
	cb.unplugged.Store(true)
	ui.Sync()

	if ui.ConnLabel.Text != "Offline" {
		t.Errorf("offline label = %q, want %q", ui.ConnLabel.Text, "Offline")
	}
	if ui.ConnDot.FillColor != theme.ErrorColor() {
		t.Errorf("offline dot color = %v, want %v", ui.ConnDot.FillColor, theme.ErrorColor())
	}
	if !strings.Contains(ui.Status.Text, "unreachable") {
		t.Errorf("status text should contain unreachable message, got %q", ui.Status.Text)
	}
}

func TestConnectionStatusImmediateFailureReactivity(t *testing.T) {
	c := newServer(t)
	cb := plug(c)
	create(t, c, "# Groceries\nmilk")
	path := filepath.Join(t.TempDir(), "cache.json")
	ui, w := openCached(t, c, path)

	if ui.ConnLabel.Text != "Connected" {
		t.Fatalf("expected initial connected status, got %q", ui.ConnLabel.Text)
	}

	// Unplug network and trigger an online-only action (Delete)
	cb.unplugged.Store(true)
	ui.List.Select(0)
	test.Tap(ui.Delete)
	tapDialogButton(t, w, "Delete")

	// Indicator must immediately flip to Offline without waiting for periodic Sync
	if ui.ConnLabel.Text != "Offline" {
		t.Errorf("expected immediate Offline on unreachable delete, got %q", ui.ConnLabel.Text)
	}
	if ui.ConnDot.FillColor != theme.ErrorColor() {
		t.Errorf("expected error dot color on unreachable delete, got %v", ui.ConnDot.FillColor)
	}
	if !strings.Contains(ui.Status.Text, "unreachable") {
		t.Errorf("expected unreachable status text, got %q", ui.Status.Text)
	}
}

func TestConnectionStatusTapRetry(t *testing.T) {
	c := newServer(t)
	cb := plug(c)
	test.NewTempApp(t)
	w := test.NewWindow(nil)
	t.Cleanup(func() {
		if slices.Contains(fyne.CurrentApp().Driver().AllWindows(), w) {
			w.Close()
		}
	})
	cache, err := offline.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}

	ui := gui.New(w, c, cache)
	cb.unplugged.Store(true)
	ui.Sync()

	if ui.ConnLabel.Text != "Offline" {
		t.Fatalf("expected Offline, got %q", ui.ConnLabel.Text)
	}

	// Restore connection
	cb.unplugged.Store(false)

	// In-flight during tap retry, indicator transitions through "Connecting..."
	inFlightChecked := false
	cb.after = func(*http.Request) {
		if ui.ConnLabel.Text != "Connecting..." {
			t.Errorf("in-flight tap retry label = %q, want %q", ui.ConnLabel.Text, "Connecting...")
		}
		if ui.ConnDot.FillColor != theme.DisabledColor() {
			t.Errorf("in-flight tap retry dot color = %v, want %v", ui.ConnDot.FillColor, theme.DisabledColor())
		}
		inFlightChecked = true
	}

	test.Tap(ui.ConnStatus)

	if !inFlightChecked {
		t.Error("expected in-flight check to run during retry")
	}
	if ui.ConnLabel.Text != "Connected" {
		t.Errorf("after tap retry label = %q, want %q", ui.ConnLabel.Text, "Connected")
	}
	if ui.ConnDot.FillColor != theme.SuccessColor() {
		t.Errorf("after tap retry dot color = %v, want %v", ui.ConnDot.FillColor, theme.SuccessColor())
	}
}

func TestConnectionStatusLayout(t *testing.T) {
	c := newServer(t)
	test.NewTempApp(t)
	w := test.NewWindow(nil)
	t.Cleanup(func() {
		if slices.Contains(fyne.CurrentApp().Driver().AllWindows(), w) {
			w.Close()
		}
	})
	cache, err := offline.Open(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}

	ui := gui.New(w, c, cache)

	root, ok := w.Content().(*fyne.Container)
	if !ok {
		t.Fatalf("expected root container, got %T", w.Content())
	}
	var statusBar *fyne.Container
	for _, obj := range root.Objects {
		if c, ok := obj.(*fyne.Container); ok && slices.Contains(c.Objects, fyne.CanvasObject(ui.ConnStatus)) {
			statusBar = c
			break
		}
	}
	if statusBar == nil {
		t.Fatal("expected status bar container containing ConnStatus")
	}
	if !slices.Contains(statusBar.Objects, fyne.CanvasObject(ui.Status)) {
		t.Error("expected status bar container to contain Status label")
	}
	if !slices.Contains(statusBar.Objects, fyne.CanvasObject(ui.ConnStatus)) {
		t.Error("expected status bar container to contain ConnStatus")
	}
}
