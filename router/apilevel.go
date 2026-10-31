package router

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/logging"
)

// APILevel is the server's API level, sent in APIHeader on every response
// and in /healthz. It goes up by one whenever endpoints or fields are added
// (ADR 8). Level 2 added note history (#55), level 3 export and import (#117).
const APILevel = 3

// minClientAPI is the lowest client API level the note endpoints accept.
// It is raised only by a breaking change: a field renamed or removed, or a
// meaning changed (ADR 8).
const minClientAPI = 1

const (
	// APIHeader carries the server's API level.
	APIHeader = "X-Scribble-API"
	// ClientHeader is how a client says who it is: "<name>/<api level>
	// <version>", e.g. "cli/1 v0.0.9".
	ClientHeader = "X-Scribble-Client"
)

// tooOld is the 426 body. Old clients print the error text they get, so it
// must make sense on its own.
const tooOld = "This Scribble client is too old for this server. Download the latest release: https://github.com/johansundell/scribble/releases/latest"

// refusalLogEvery is how often a refused client string is logged again.
const refusalLogEvery = time.Hour

// clientLevel is the API level in an X-Scribble-Client value. A missing or
// unreadable header counts as level 1: every client released before the
// header existed.
func clientLevel(header string) int {
	nameLevel, _, _ := strings.Cut(header, " ")
	_, level, ok := strings.Cut(nameLevel, "/")
	if n, err := strconv.Atoi(level); ok && err == nil && n > 0 {
		return n
	}
	return 1
}

// ClientCheck refuses clients whose API level is below minLevel with 426
// Upgrade Required. Each refusal is logged as a warning, at most once per
// client string per hour; now is the clock (time.Now when nil).
func ClientCheck(minLevel int, l Logger, now func() time.Time) func(HandlerFuncWithError) HandlerFuncWithError {
	l = logging.OrStd(l)
	if now == nil {
		now = time.Now
	}
	var mu sync.Mutex
	logged := map[string]time.Time{}
	return func(inner HandlerFuncWithError) HandlerFuncWithError {
		return func(c *gin.Context) error {
			who := c.GetHeader(ClientHeader)
			level := clientLevel(who)
			if level >= minLevel {
				return inner(c)
			}
			mu.Lock()
			t := now()
			if last, ok := logged[who]; !ok || t.Sub(last) >= refusalLogEvery {
				for k, v := range logged {
					if t.Sub(v) >= refusalLogEvery {
						delete(logged, k)
					}
				}
				logged[who] = t
				l.Warningf("refused a client too old for this server (API level %d, minimum %d): %q from %s", level, minLevel, who, c.ClientIP())
			}
			mu.Unlock()
			c.String(http.StatusUpgradeRequired, tooOld)
			return nil
		}
	}
}

// apiLevelHeader sends the API level on every response and hands it to
// handlers (for /healthz).
func apiLevelHeader(level int) gin.HandlerFunc {
	v := strconv.Itoa(level)
	return func(c *gin.Context) {
		c.Header(APIHeader, v)
		c.Set(handlers.APILevelKey, level)
		c.Next()
	}
}
