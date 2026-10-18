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
	"time"

	"github.com/johansundell/scribble/notes"
)

// Client talks to one scribble server.
type Client struct {
	BaseURL string // e.g. http://localhost:8080
	Token   string // the server's AUTH_TOKEN
	HTTP    *http.Client
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
	Status  int
	Message string
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
	resp, err := c.HTTP.Do(req)
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
	var out []notes.Note
	query := url.Values{}
	if q != "" {
		query.Set("q", q)
	}
	return out, c.do(ctx, "GET", "/notes", query, nil, &out)
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
