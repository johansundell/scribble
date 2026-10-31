package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"github.com/johansundell/scribble/offline"
)

func TestNewAppLoadsDotEnvBeforeCreatingTheApp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("FYNE_THEME=dark\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FYNE_THEME", "")
	os.Unsetenv("FYNE_THEME")

	var themeAtCreate string
	a, envErr := newApp(func() error { return loadEnv(dir) }, func() fyne.App {
		themeAtCreate = os.Getenv("FYNE_THEME")
		return test.NewTempApp(t)
	})
	if envErr != nil {
		t.Fatal(envErr)
	}
	if a == nil {
		t.Fatal("newApp returned no app")
	}
	if themeAtCreate != "dark" {
		t.Errorf("FYNE_THEME when the app was created = %q, want %q", themeAtCreate, "dark")
	}
}

func TestNewAppStillCreatesTheAppWhenDotEnvIsUnreadable(t *testing.T) {
	dir := t.TempDir()
	// A directory named .env cannot be read as a file.
	if err := os.Mkdir(filepath.Join(dir, ".env"), 0o700); err != nil {
		t.Fatal(err)
	}

	a, envErr := newApp(func() error { return loadEnv(dir) }, func() fyne.App {
		return test.NewTempApp(t)
	})
	if envErr == nil {
		t.Error("unreadable .env: got no error")
	}
	if a == nil {
		t.Fatal("newApp returned no app; the error could not be shown")
	}
}

func TestLoadEnvReadsDotEnvInDir(t *testing.T) {
	dir := t.TempDir()
	env := "SCRIBBLE_URL=http://notes:8080\nSCRIBBLE_TOKEN=from-file\nSCRIBBLE_HOTKEY=none\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCRIBBLE_URL", "")
	os.Unsetenv("SCRIBBLE_URL")
	t.Setenv("SCRIBBLE_HOTKEY", "")
	os.Unsetenv("SCRIBBLE_HOTKEY")
	t.Setenv("SCRIBBLE_TOKEN", "from-environment")

	if err := loadEnv(dir); err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]string{
		"SCRIBBLE_URL":    "http://notes:8080",
		"SCRIBBLE_HOTKEY": "none",
		"SCRIBBLE_TOKEN":  "from-environment", // already set: the environment wins
	} {
		if got := os.Getenv(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestLoadEnvWithoutDotEnvIsFine(t *testing.T) {
	if err := loadEnv(t.TempDir()); err != nil {
		t.Fatalf("missing .env: %v", err)
	}
}

func TestOpenCacheLocksAndPreventsSecondInstance(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	first, err := openCache("http://localhost:8080")
	if err != nil {
		t.Fatalf("first openCache: %v", err)
	}
	defer first.Close()

	second, err := openCache("http://localhost:8080")
	if err == nil {
		_ = second.Close()
		t.Fatal("second openCache succeeded, want ErrLocked")
	}
	if !errors.Is(err, offline.ErrLocked) {
		t.Fatalf("second openCache error %v, want ErrLocked", err)
	}
}

// appWithIcon keeps its icon, which the test app drops.
type appWithIcon struct {
	fyne.App
	icon fyne.Resource
}

func (a *appWithIcon) Icon() fyne.Resource     { return a.icon }
func (a *appWithIcon) SetIcon(r fyne.Resource) { a.icon = r }

func TestNewAppSetsTheAppIcon(t *testing.T) {
	a, _ := newApp(func() error { return nil }, func() fyne.App { return &appWithIcon{App: test.NewTempApp(t)} })
	icon := a.Icon()
	if icon == nil {
		t.Fatal("the app has no icon")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(icon.Content()))
	if err != nil {
		t.Fatalf("the app icon is not a PNG: %v", err)
	}
	if cfg.Width != 512 || cfg.Height != 512 {
		t.Errorf("the app icon is %dx%d, want 512x512", cfg.Width, cfg.Height)
	}
}

// appMakingIconWindows hands out windows that keep their icon, which test
// windows drop.
type appMakingIconWindows struct {
	appWithIcon
}

type windowWithIcon struct {
	fyne.Window
	icon fyne.Resource
}

func (w *windowWithIcon) Icon() fyne.Resource     { return w.icon }
func (w *windowWithIcon) SetIcon(r fyne.Resource) { w.icon = r }

func (a *appMakingIconWindows) NewWindow(title string) fyne.Window {
	return &windowWithIcon{Window: a.App.NewWindow(title)}
}

func TestNewWindowSetsTheAppIconOnTheWindow(t *testing.T) {
	icon := fyne.NewStaticResource("Icon.png", iconPNG)
	a := &appMakingIconWindows{appWithIcon: appWithIcon{App: test.NewTempApp(t), icon: icon}}

	w := newWindow(a, "Scribble test")

	if w.Title() != "Scribble test" {
		t.Errorf("window title = %q, want %q", w.Title(), "Scribble test")
	}
	if got := w.Icon(); got != icon {
		t.Errorf("window icon = %v, want the app icon", got)
	}
}

// icon.ico, which the Windows tray embeds, is the multi-size app icon that
// make icons copies from assets/img (#120). Checked here so it runs on every
// platform, not only on Windows.
func TestIconICOIsTheAppIcon(t *testing.T) {
	ico, err := os.ReadFile("icon.ico")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(ico, []byte{0x00, 0x00, 0x01, 0x00}) {
		t.Error("icon.ico does not start with the ICO header")
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "assets", "img", "icon.ico"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ico, want) {
		t.Error("icon.ico differs from assets/img/icon.ico; run make icons")
	}
}

// icoSizes reads the image sizes from an ICO directory. A width or height
// byte of 0 means 256.
func icoSizes(t *testing.T, ico []byte) []int {
	t.Helper()
	if len(ico) < 6 {
		t.Fatalf("ICO is %d bytes, too short for its header", len(ico))
	}
	n := int(binary.LittleEndian.Uint16(ico[4:6]))
	if len(ico) < 6+16*n {
		t.Fatalf("ICO says %d images but is only %d bytes", n, len(ico))
	}
	var sizes []int
	for i := range n {
		e := ico[6+16*i:]
		w, h := int(e[0]), int(e[1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		if w != h {
			t.Errorf("ICO image %d is %dx%d, want square", i, w, h)
		}
		sizes = append(sizes, w)
	}
	slices.Sort(sizes)
	return sizes
}

// go-winres puts every size of icon.ico into scribble-gui.exe, since Fyne's
// packager keeps only the largest (#123).
func TestIconICOHasEverySize(t *testing.T) {
	ico, err := os.ReadFile("icon.ico")
	if err != nil {
		t.Fatal(err)
	}
	want := []int{16, 24, 32, 48, 64, 128, 256}
	if got := icoSizes(t, ico); !slices.Equal(got, want) {
		t.Errorf("icon.ico sizes = %v, want %v", got, want)
	}
}

// winres.json replaces the single-size icon groups that fyne-cross writes
// (#2 and IDI_APPLICATION, #32512) with icon.ico, and leaves its RT_VERSION
// and RT_MANIFEST alone (#123).
func TestWinresReplacesBothIconGroupsWithIconICO(t *testing.T) {
	raw, err := os.ReadFile("winres.json")
	if err != nil {
		t.Fatal(err)
	}
	var res map[string]map[string]map[string]string
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("winres.json: %v", err)
	}
	for _, id := range []string{"#2", "#32512"} {
		if got := res["RT_GROUP_ICON"][id]["0409"]; got != "icon.ico" {
			t.Errorf("RT_GROUP_ICON %s 0409 = %q, want %q", id, got, "icon.ico")
		}
	}
	for typ := range res {
		if typ != "RT_GROUP_ICON" {
			t.Errorf("winres.json sets %s; only RT_GROUP_ICON should change", typ)
		}
	}
}
