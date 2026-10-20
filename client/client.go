// Package client is a small Go client for the scribble server API. The CLI
// and the GUI both use it.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/johansundell/scribble/notes"
)

// APILevel is the server API level this client is written for. The CLI and
// the GUI share it. Raise it with the server's when the client starts using
// what a new level adds (ADR 8).
const APILevel = 1

// ReleasesURL is where the latest release can be downloaded.
const ReleasesURL = "https://github.com/johansundell/scribble/releases/latest"

// Client talks to one scribble server.
type Client struct {
	BaseURL string // e.g. http://localhost:8080
	Token   string // the server's AUTH_TOKEN
	HTTP    *http.Client

	// Name, APILevel and Version are sent in X-Scribble-Client, so the
	// server can refuse clients that are too old. Name is "cli" or "gui";
	// APILevel 0 means the APILevel constant, and Version "" means "dev".
	Name     string
	APILevel int
	Version  string

	mu            sync.Mutex
	serverVersion string // X-Version of the last answer
	serverAPI     int    // X-Scribble-API of the last answer
}

// New returns a Client with a sensible timeout.
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Error is a non-2xx answer from the server. The server replies with the
// plain HTTP status text, so Message is short ("Not Found", ...).
type Error struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("server said %d %s", e.Status, e.Message) }

func hasStatus(err error, status int) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == status
}

func IsNotFound(err error) bool     { return hasStatus(err, http.StatusNotFound) }
func IsTitleTaken(err error) bool   { return hasStatus(err, http.StatusConflict) }
func IsStale(err error) bool        { return hasStatus(err, http.StatusPreconditionFailed) }
func IsUnauthorized(err error) bool { return hasStatus(err, http.StatusUnauthorized) }

// IsUpgradeRequired reports whether the server refused this client as too
// old for its API (426).
func IsUpgradeRequired(err error) bool { return hasStatus(err, http.StatusUpgradeRequired) }

// IsUnreachable reports whether err is a failure to get an answer from the
// server at all (no network, server down, timeout), rather than a refusal.
func IsUnreachable(err error) bool {
	var e *url.Error
	return errors.As(err, &e)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	u := c.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return &Error{Status: resp.StatusCode, Message: strings.TrimSpace(string(msg))}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func notePath(id int64, suffix string) string {
	return "/notes/" + strconv.FormatInt(id, 10) + suffix
}

// List returns notes, newest first. A non-empty q searches titles and bodies.
func (c *Client) List(ctx context.Context, q string) ([]notes.Note, error) {
	return c.ListInProject(ctx, "", q)
}

// ListInProject is List narrowed to notes tagged #project ("#name" or
// "name"); an empty project lists every note.
func (c *Client) ListInProject(ctx context.Context, project, q string) ([]notes.Note, error) {
	var out []notes.Note
	query := url.Values{}
	if q != "" {
		query.Set("q", q)
	}
	if project != "" {
		query.Set("project", project)
	}
	return out, c.do(ctx, "GET", "/notes", query, nil, &out)
}

// Projects returns the #project names in use, lowercase and sorted.
func (c *Client) Projects(ctx context.Context) ([]string, error) {
	var out []string
	return out, c.do(ctx, "GET", "/projects", nil, nil, &out)
}

// ByTitle finds a note by exact (case-insensitive) title. It returns an
// *Error with Status 404 when there is none, so IsNotFound works.
func (c *Client) ByTitle(ctx context.Context, title string) (*notes.Note, error) {
	var out []notes.Note
	if err := c.do(ctx, "GET", "/notes", url.Values{"title": {title}}, nil, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, &Error{Status: http.StatusNotFound, Message: "Not Found"}
	}
	return &out[0], nil
}

func (c *Client) Get(ctx context.Context, id int64) (*notes.Note, error) {
	var n notes.Note
	if err := c.do(ctx, "GET", notePath(id, ""), nil, nil, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// Create makes a new note; its title is the first line of body.
func (c *Client) Create(ctx context.Context, body string) (*notes.Note, error) {
	var n notes.Note
	err := c.do(ctx, "POST", "/notes", nil, map[string]any{"body": body}, &n)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// Update replaces a note's body. Pass the revision you edited from as
// baseRevision; if someone else saved since, IsStale(err) is true. Pass 0 to
// overwrite regardless.
func (c *Client) Update(ctx context.Context, id int64, body string, baseRevision int64) (*notes.Note, error) {
	var n notes.Note
	err := c.do(ctx, "PUT", notePath(id, ""), nil,
		map[string]any{"body": body, "base_revision": baseRevision}, &n)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (c *Client) Delete(ctx context.Context, id int64) error {
	return c.do(ctx, "DELETE", notePath(id, ""), nil, nil, nil)
}

// Backlinks returns the notes that contain a [[link]] to note id.
func (c *Client) Backlinks(ctx context.Context, id int64) ([]notes.Note, error) {
	var out []notes.Note
	return out, c.do(ctx, "GET", notePath(id, "/backlinks"), nil, nil, &out)
}

// Rename retitles a note and rewrites [[links]] to it in other notes.
func (c *Client) Rename(ctx context.Context, id int64, title string) (*notes.Note, error) {
	var n notes.Note
	err := c.do(ctx, "POST", notePath(id, "/rename"), nil, map[string]string{"title": title}, &n)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// Changes returns everything modified after sequence number since (deleted
// notes have Deleted set) and the cursor to pass next time.
func (c *Client) Changes(ctx context.Context, since int64) (changed []notes.Note, latestSeq int64, err error) {
	var resp struct {
		Notes     []notes.Note `json:"notes"`
		LatestSeq int64        `json:"latest_seq"`
	}
	err = c.do(ctx, "GET", "/changes", url.Values{"since": {strconv.FormatInt(since, 10)}}, nil, &resp)
	return resp.Notes, resp.LatestSeq, err
}

// Versions returns note id's kept versions, newest first, without bodies
// (server API level 2).
func (c *Client) Versions(ctx context.Context, id int64) ([]notes.Version, error) {
	var out []notes.Version
	return out, c.do(ctx, "GET", notePath(id, "/versions"), nil, nil, &out)
}

// GetVersion returns note id's version at revision rev, with its body.
// IsNotFound(err) is true when that revision was not kept. (Version is
// taken: it is the client's own release.)
func (c *Client) GetVersion(ctx context.Context, id, rev int64) (*notes.Version, error) {
	var v notes.Version
	if err := c.do(ctx, "GET", notePath(id, "/versions/"+strconv.FormatInt(rev, 10)), nil, nil, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// Restore makes note id's version at rev its text again, as a new revision.
// baseRevision works as in Update; IsTitleTaken(err) is true when another
// note has the old title, or the note is deleted (Undelete it first).
func (c *Client) Restore(ctx context.Context, id, rev, baseRevision int64) (*notes.Note, error) {
	var n notes.Note
	err := c.do(ctx, "POST", notePath(id, "/versions/"+strconv.FormatInt(rev, 10)+"/restore"), nil,
		map[string]any{"base_revision": baseRevision}, &n)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// Deleted returns the deleted notes, most recently deleted first.
func (c *Client) Deleted(ctx context.Context) ([]notes.DeletedNote, error) {
	var out []notes.DeletedNote
	return out, c.do(ctx, "GET", "/notes/deleted", nil, nil, &out)
}

// Undelete brings deleted note id back with its last text. IsTitleTaken(err)
// is true when a live note has its title, IsNotFound(err) when id is not a
// deleted note.
func (c *Client) Undelete(ctx context.Context, id int64) (*notes.Note, error) {
	var n notes.Note
	if err := c.do(ctx, "POST", notePath(id, "/undelete"), nil, nil, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// send adds X-Scribble-Client to req, sends it and notes the server's
// version and API level from the answer.
func (c *Client) send(req *http.Request) (*http.Response, error) {
	level, version := c.APILevel, c.Version
	if level == 0 {
		level = APILevel
	}
	if version == "" {
		version = "dev"
	}
	req.Header.Set("X-Scribble-Client", fmt.Sprintf("%s/%d %s", c.Name, level, version))
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	// An answer without the headers (a proxy's 502, say) keeps what we knew.
	if v := resp.Header.Get("X-Version"); v != "" {
		api, _ := strconv.Atoi(resp.Header.Get("X-Scribble-API"))
		c.mu.Lock()
		c.serverVersion, c.serverAPI = v, api
		c.mu.Unlock()
	}
	return resp, nil
}

// ServerVersion is the server's version and API level as of its last
// answer: "" and 0 before the first one.
func (c *Client) ServerVersion() (version string, api int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.serverVersion, c.serverAPI
}

// Health is what the server reports at /healthz.
type Health struct {
	Version string `json:"version"`
	API     int    `json:"api"`
}

// Health asks the server for its version and API level. It needs no token,
// and the server answers it whatever the client's level.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var h Health
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/healthz", nil)
	if err != nil {
		return h, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.send(req)
	if err != nil {
		return h, err
	}
	defer resp.Body.Close()
	// 503 means the server's storage is down; it still says who it is.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return h, &Error{Status: resp.StatusCode, Message: strings.TrimSpace(string(msg))}
	}
	return h, json.NewDecoder(resp.Body).Decode(&h)
}

// NewerRelease reports whether server is a newer release than own. Both are
// semver versions ("v1.2.3", the "v" optional); anything after the patch
// number is ignored. "dev" and versions that don't parse are never newer,
// nor older.
func NewerRelease(own, server string) bool {
	a, okA := parseVersion(own)
	b, okB := parseVersion(server)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return b[i] > a[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
