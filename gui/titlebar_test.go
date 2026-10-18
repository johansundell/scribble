package gui_test

import (
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"github.com/johansundell/scribble/gui"
)

// titleBarSpy records each title bar change MatchTitleBar asks for.
type titleBarSpy struct {
	spyWindow
	dark []bool
}

func (s *titleBarSpy) RunNative(f func(any)) {
	f(s)
}

func (s *titleBarSpy) SetTitleBarDark(dark bool) {
	s.dark = append(s.dark, dark)
}

// fakeSettings is fyne.Settings with a theme variant the test controls.
type fakeSettings struct {
	fyne.Settings
	variant   fyne.ThemeVariant
	listeners []func(fyne.Settings)
}

func (s *fakeSettings) ThemeVariant() fyne.ThemeVariant { return s.variant }

func (s *fakeSettings) AddListener(l func(fyne.Settings)) {
	s.listeners = append(s.listeners, l)
}

func (s *fakeSettings) change(variant fyne.ThemeVariant) {
	s.variant = variant
	for _, l := range s.listeners {
		l(s)
	}
}

func TestMatchTitleBarFollowsThemeVariant(t *testing.T) {
	a := test.NewTempApp(t)
	w := &titleBarSpy{spyWindow: spyWindow{Window: a.NewWindow("t")}}
	s := &fakeSettings{Settings: a.Settings(), variant: theme.VariantDark}

	gui.MatchTitleBar(w, s)
	if want := []bool{true}; !slices.Equal(w.dark, want) {
		t.Fatalf("after MatchTitleBar with a dark theme: got %v, want %v", w.dark, want)
	}

	s.change(theme.VariantLight)
	s.change(theme.VariantDark)
	if want := []bool{true, false, true}; !slices.Equal(w.dark, want) {
		t.Fatalf("after switching to light and back: got %v, want %v", w.dark, want)
	}
}

func TestMatchTitleBarIgnoresNonNativeWindow(t *testing.T) {
	a := test.NewTempApp(t)
	s := &fakeSettings{Settings: a.Settings(), variant: theme.VariantDark}

	gui.MatchTitleBar(a.NewWindow("t"), s) // must not panic
	if len(s.listeners) != 0 {
		t.Fatalf("MatchTitleBar added %d settings listeners for a window with no native handle", len(s.listeners))
	}
}
