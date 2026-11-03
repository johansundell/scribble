package router_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/johansundell/scribble/handlers"
)

// newHintEnv is newWebEnv with WEB_LOGIN_HINT set to hint (#168).
func newHintEnv(t *testing.T, hint string) *webEnv {
	t.Helper()
	e := newWebEnv(t)
	e.r = newWebRouter(t, e.store, e.auth, e.log, nil, handlers.WithLoginHint(hint))
	return e
}

func TestLoginHintIsShownUnderThePINField(t *testing.T) {
	e := newHintEnv(t, "Demo: the PIN is 123456. <b>Reset</b> nightly.")
	want := `<p class="login-hint" id="login-hint">Demo: the PIN is 123456. &lt;b&gt;Reset&lt;/b&gt; nightly.</p>`

	w := e.do(get("/login"))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
		t.Fatalf("GET /login = %d, want the escaped hint %s in:\n%s", w.Code, want, w.Body.String())
	}
	body := w.Body.String()
	if field, hint := strings.Index(body, `id="pin"`), strings.Index(body, want); hint < field {
		t.Error("the hint comes before the PIN field")
	}
	// Screen readers announce it with the field.
	if !strings.Contains(body, `aria-describedby="login-hint"`) {
		t.Error("the PIN field does not point at the hint")
	}
	// Still there after a wrong PIN.
	if w := e.do(postLogin("10.0.0.1", "000000", "")); !strings.Contains(w.Body.String(), want) {
		t.Errorf("wrong PIN = %d, hint missing:\n%s", w.Code, w.Body.String())
	}
}

func TestNoLoginHintByDefault(t *testing.T) {
	for _, e := range []*webEnv{newWebEnv(t), newHintEnv(t, "")} {
		if body := e.do(get("/login")).Body.String(); strings.Contains(body, "login-hint") || strings.Contains(body, "aria-describedby") {
			t.Errorf("hint shown without WEB_LOGIN_HINT:\n%s", body)
		}
	}
}
