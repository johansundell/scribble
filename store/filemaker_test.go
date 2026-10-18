package store

import (
	"bufio"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/johansundell/scribble/types"
)

// fakeFileMaker is a minimal FileMaker OData server for the log table.
type fakeFileMaker struct {
	mu          sync.Mutex
	checkStatus int              // status for the startup $select check (0 = 200)
	batchStatus int              // status for each create in a $batch (0 = 204)
	creates     []map[string]any // records received through $batch
	prefer      []string         // Prefer header of each create
	lastQuery   map[string]string
	records     []map[string]any // returned by filtered reads
}

func (f *fakeFileMaker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if user, pass, ok := r.BasicAuth(); !ok || user != "admin" || pass != "secret" {
		http.Error(w, `{"error":{"code":"212","message":"Invalid account"}}`, http.StatusUnauthorized)
		return
	}
	base := "/fmi/odata/v4/Logging"
	switch {
	case r.Method == http.MethodGet && r.URL.Path == base:
		json.NewEncoder(w).Encode(map[string]any{"value": []any{}})
	case r.Method == http.MethodGet && r.URL.Path == base+"/Logs":
		q := r.URL.Query()
		f.lastQuery = map[string]string{"$select": q.Get("$select"), "$filter": q.Get("$filter"), "$orderby": q.Get("$orderby"), "$top": q.Get("$top"), "$skip": q.Get("$skip")}
		if q.Get("$top") == "0" {
			if f.checkStatus != 0 {
				http.Error(w, `{"error":{"code":"-1","message":"field Endpoint not found"}}`, f.checkStatus)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"value": []any{}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"value": f.records})
	case r.Method == http.MethodPost && r.URL.Path == base+"/$batch":
		f.handleBatch(w, r)
	default:
		http.Error(w, "not found: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	}
}

// handleBatch parses a $batch request with one change set of creates and
// answers with one response per create.
func (f *fakeFileMaker) handleBatch(w http.ResponseWriter, r *http.Request) {
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		http.Error(w, "bad batch content type", http.StatusBadRequest)
		return
	}
	batch := multipart.NewReader(r.Body, params["boundary"])
	changesetPart, err := batch.NextPart()
	if err != nil {
		http.Error(w, "no change set", http.StatusBadRequest)
		return
	}
	_, csParams, _ := mime.ParseMediaType(changesetPart.Header.Get("Content-Type"))
	changeset := multipart.NewReader(changesetPart, csParams["boundary"])

	var statuses []int
	for {
		part, err := changeset.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			http.Error(w, "bad change set: "+err.Error(), http.StatusBadRequest)
			return
		}
		req, err := http.ReadRequest(bufio.NewReader(part))
		if err != nil || req.Method != http.MethodPost || !strings.HasSuffix(req.URL.Path, "/Logs") {
			http.Error(w, fmt.Sprintf("bad inner request: %v", err), http.StatusBadRequest)
			return
		}
		var record map[string]any
		json.NewDecoder(req.Body).Decode(&record)
		f.creates = append(f.creates, record)
		f.prefer = append(f.prefer, req.Header.Get("Prefer"))
		status := f.batchStatus
		if status == 0 {
			status = http.StatusNoContent
		}
		statuses = append(statuses, status)
	}

	const outer, inner = "batchresponse_1", "changesetresponse_1"
	w.Header().Set("Content-Type", "multipart/mixed; boundary="+outer)
	fmt.Fprintf(w, "--%s\r\nContent-Type: multipart/mixed; boundary=%s\r\n\r\n", outer, inner)
	for i, status := range statuses {
		fmt.Fprintf(w, "--%s\r\nContent-Type: application/http\r\nContent-Transfer-Encoding: binary\r\nContent-ID: %d\r\n\r\n", inner, i+1)
		fmt.Fprintf(w, "HTTP/1.1 %d %s\r\n", status, http.StatusText(status))
		if status >= 400 {
			body := `{"error":{"code":"-1","message":"rejected"}}`
			fmt.Fprintf(w, "Content-Type: application/json\r\nContent-Length: %d\r\n\r\n%s\r\n", len(body), body)
		} else {
			w.Write([]byte("\r\n\r\n"))
		}
	}
	fmt.Fprintf(w, "--%s--\r\n--%s--\r\n", inner, outer)
}

// startFakeFileMaker serves f over TLS and returns a config that trusts it
// through CAFile.
func startFakeFileMaker(t *testing.T, f *fakeFileMaker) types.FileMakerSettings {
	t.Helper()
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(caFile, certPEM, 0o600); err != nil {
		t.Fatalf("write CA file: %v", err)
	}
	return types.FileMakerSettings{
		Host: srv.URL, Database: "Logging", Username: "admin", Password: "secret",
		Timeout: 5 * time.Second, LogTable: "Logs", CAFile: caFile,
	}
}

func newTestFileMaker(t *testing.T, f *fakeFileMaker) *FileMakerStore {
	t.Helper()
	s, err := NewFileMaker(context.Background(), startFakeFileMaker(t, f))
	if err != nil {
		t.Fatalf("NewFileMaker failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNewFileMaker_ChecksTableAndFields(t *testing.T) {
	f := &fakeFileMaker{}
	newTestFileMaker(t, f)

	if got, want := f.lastQuery["$select"], "ID,Status,Method,Error,Endpoint,CreatedAt,Request,Response"; got != want {
		t.Errorf("expected startup check to select %q, got %q", want, got)
	}
	if f.lastQuery["$top"] != "0" {
		t.Errorf("expected startup check with $top=0, got %q", f.lastQuery["$top"])
	}
}

func TestNewFileMaker_FailsOnMissingField(t *testing.T) {
	cfg := startFakeFileMaker(t, &fakeFileMaker{checkStatus: http.StatusBadRequest})
	_, err := NewFileMaker(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "field Endpoint not found") || !strings.Contains(err.Error(), `"Logs"`) {
		t.Fatalf("expected startup to fail with FileMaker's error, got %v", err)
	}
}

func TestNewFileMaker_FailsOnBadCredentials(t *testing.T) {
	cfg := startFakeFileMaker(t, &fakeFileMaker{})
	cfg.Password = "wrong"
	_, err := NewFileMaker(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("expected a 401 error, got %v", err)
	}
}

func TestNewFileMaker_TLS(t *testing.T) {
	cfg := startFakeFileMaker(t, &fakeFileMaker{})

	t.Run("untrusted certificate fails", func(t *testing.T) {
		c := cfg
		c.CAFile = ""
		if _, err := NewFileMaker(context.Background(), c); err == nil || !strings.Contains(err.Error(), "certificate") {
			t.Fatalf("expected a certificate error without the CA file, got %v", err)
		}
	})
	t.Run("insecure skip verify", func(t *testing.T) {
		c := cfg
		c.CAFile = ""
		c.InsecureSkipVerify = true
		s, err := NewFileMaker(context.Background(), c)
		if err != nil {
			t.Fatalf("expected InsecureSkipVerify to connect, got %v", err)
		}
		s.Close()
	})
	t.Run("CA file without certificates", func(t *testing.T) {
		c := cfg
		c.CAFile = filepath.Join(t.TempDir(), "empty.pem")
		os.WriteFile(c.CAFile, []byte("not a certificate"), 0o600)
		if _, err := NewFileMaker(context.Background(), c); err == nil || !strings.Contains(err.Error(), "no PEM certificates") {
			t.Fatalf("expected a PEM error, got %v", err)
		}
	})
}

func TestFileMaker_Ping(t *testing.T) {
	s := newTestFileMaker(t, &fakeFileMaker{})
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestFileMaker_LogRequestsSendsOneBatch(t *testing.T) {
	f := &fakeFileMaker{}
	s := newTestFileMaker(t, f)

	cest := time.FixedZone("CEST", 2*60*60)
	entries := []types.UsageLog{
		{Status: 200, Method: "GET", Endpoint: "/ping/a", CreatedAt: time.Date(2026, 9, 30, 12, 15, 30, 0, cest), Request: "{}", Response: `{"result":"a"}`},
		{Status: 400, Method: "POST", Error: "bad", Endpoint: "/pong", CreatedAt: time.Date(2026, 9, 30, 12, 16, 0, 0, cest), Request: "[]", Response: "Bad Request"},
	}
	if err := s.LogRequests(context.Background(), entries); err != nil {
		t.Fatalf("LogRequests failed: %v", err)
	}

	if len(f.creates) != 2 {
		t.Fatalf("expected 2 creates in the batch, got %d", len(f.creates))
	}
	first := f.creates[0]
	if first["CreatedAt"] != "2026-09-30T10:15:30" {
		t.Errorf("expected CreatedAt in UTC without offset, got %v", first["CreatedAt"])
	}
	if first["Status"] != float64(200) || first["Endpoint"] != "/ping/a" || first["Response"] != `{"result":"a"}` {
		t.Errorf("unexpected first record %v", first)
	}
	if _, hasID := first["ID"]; hasID {
		t.Errorf("expected ID to be left to FileMaker's auto-enter, got %v", first["ID"])
	}
	if f.creates[1]["Error"] != "bad" {
		t.Errorf("unexpected second record %v", f.creates[1])
	}
	for i, p := range f.prefer {
		if p != "return=minimal" {
			t.Errorf("create %d: expected Prefer: return=minimal, got %q", i+1, p)
		}
	}
}

func TestFileMaker_LogRequestsClassifiesErrors(t *testing.T) {
	cases := []struct {
		status    int
		permanent bool
	}{
		{http.StatusBadRequest, true},
		{http.StatusUnauthorized, true},
		{http.StatusRequestTimeout, false},
		{http.StatusTooManyRequests, false},
		{http.StatusInternalServerError, false},
		{http.StatusServiceUnavailable, false},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			f := &fakeFileMaker{}
			s := newTestFileMaker(t, f)
			f.mu.Lock()
			f.batchStatus = tc.status
			f.mu.Unlock()

			err := s.LogRequests(context.Background(), []types.UsageLog{{Endpoint: "/x", CreatedAt: time.Now()}})
			if err == nil {
				t.Fatal("expected an error")
			}
			if IsPermanent(err) != tc.permanent {
				t.Errorf("status %d: expected permanent=%v, got %v (%v)", tc.status, tc.permanent, IsPermanent(err), err)
			}
		})
	}
}

func TestFileMaker_GetLogs(t *testing.T) {
	f := &fakeFileMaker{records: []map[string]any{
		{"ID": float64(7), "Status": float64(200), "Method": "GET", "Error": nil, "Endpoint": "/ping/a", "CreatedAt": "2026-09-30T10:15:30", "Request": "{}", "Response": `{"result":"a"}`},
		// FileMaker may attach the server's offset on the way back; the wall clock is UTC.
		{"ID": float64(8), "Status": float64(404), "Method": "GET", "Error": "Nope", "Endpoint": "/ping/notfound", "CreatedAt": "2026-09-30T22:30:00+02:00", "Request": "{}", "Response": "Not Found"},
	}}
	s := newTestFileMaker(t, f)

	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	logs, err := s.GetLogs(context.Background(), day, day.AddDate(0, 0, 1), Page{})
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}

	if got, want := f.lastQuery["$filter"], "CreatedAt ge 2026-09-30T00:00:00 and CreatedAt lt 2026-10-01T00:00:00"; got != want {
		t.Errorf("expected filter %q, got %q", want, got)
	}
	if got := f.lastQuery["$orderby"]; got != "CreatedAt asc,ID asc" {
		t.Errorf("unexpected $orderby %q", got)
	}

	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}
	first := logs[0]
	if first.ID != 7 || first.Status != 200 || first.Endpoint != "/ping/a" || first.Error != "" || string(first.Response) != `{"result":"a"}` {
		t.Errorf("unexpected first log %+v", first)
	}
	if !first.CreatedAt.Equal(time.Date(2026, 9, 30, 10, 15, 30, 0, time.UTC)) {
		t.Errorf("expected CreatedAt 10:15:30Z, got %v", first.CreatedAt)
	}
	if !logs[1].CreatedAt.Equal(time.Date(2026, 9, 30, 22, 30, 0, 0, time.UTC)) {
		t.Errorf("expected the returned offset to be ignored (22:30Z), got %v", logs[1].CreatedAt)
	}
}

func TestParseFileMakerTime_Invalid(t *testing.T) {
	if _, err := parseFileMakerTime("yesterday"); err == nil {
		t.Error("expected an error for an unrecognized timestamp")
	}
}

func TestFileMaker_GetLogsPage(t *testing.T) {
	f := &fakeFileMaker{}
	s := newTestFileMaker(t, f)
	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

	if _, err := s.GetLogs(context.Background(), day, day.AddDate(0, 0, 1), Page{Limit: 3, Offset: 6}); err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if f.lastQuery["$top"] != "3" || f.lastQuery["$skip"] != "6" {
		t.Errorf("expected $top=3 and $skip=6, got $top=%q $skip=%q", f.lastQuery["$top"], f.lastQuery["$skip"])
	}

	if _, err := s.GetLogs(context.Background(), day, day.AddDate(0, 0, 1), Page{}); err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if f.lastQuery["$top"] != "" || f.lastQuery["$skip"] != "" {
		t.Errorf("expected no $top/$skip without a page, got $top=%q $skip=%q", f.lastQuery["$top"], f.lastQuery["$skip"])
	}
}
