package store

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/johansundell/scribble/fmsodata"
	"github.com/johansundell/scribble/types"
)

// logTableFields are the log table's fields, in the order they are selected,
// with the types startup creates them with. OData can't make ID an
// auto-enter serial, so in a created table it stays empty until a developer
// changes it in FileMaker.
var logTableFields = []fmsodata.FieldDefinition{
	{Name: "ID", Type: "NUMERIC"},
	{Name: "Status", Type: "NUMERIC"},
	{Name: "Method", Type: "VARCHAR"},
	{Name: "Error", Type: "VARCHAR"},
	{Name: "Endpoint", Type: "VARCHAR"},
	{Name: "CreatedAt", Type: "TIMESTAMP"},
	{Name: "Request", Type: "VARCHAR"},
	{Name: "Response", Type: "VARCHAR"},
}

var fileMakerSelectFields = func() string {
	names := make([]string, len(logTableFields))
	for i, f := range logTableFields {
		names[i] = f.Name
	}
	return strings.Join(names, ",")
}()

// fileMakerTimeLayout writes CreatedAt as UTC without an offset: FileMaker
// timestamps have no time zone, and an offset is read relative to the
// server's zone.
const fileMakerTimeLayout = "2006-01-02T15:04:05"

// FileMakerStore keeps request logs in a FileMaker table through the OData API.
type FileMakerStore struct {
	client  *fmsodata.Client
	table   string
	created bool
}

// NewFileMaker connects to FileMaker Server and checks, within ctx, that the
// log table (cfg.LogTable) and all its fields can be read. With
// cfg.CreateLogTable it creates the table when FileMaker says it doesn't
// exist (#113, ADR 12); any other error, and an existing table, are left as
// they are. cfg.CAFile adds trusted CAs and cfg.InsecureSkipVerify disables
// certificate checks.
func NewFileMaker(ctx context.Context, cfg types.FileMakerSettings) (*FileMakerStore, error) {
	client, err := NewFileMakerClient(cfg)
	if err != nil {
		return nil, err
	}
	s := &FileMakerStore{client: client, table: cfg.LogTable}
	err = s.checkTable(ctx)
	if err != nil && cfg.CreateLogTable && isMissingTable(err) {
		if err = s.client.CreateTable(ctx, logTableDefinition(s.table)); err != nil {
			err = fmt.Errorf("create FileMaker log table %q: %w", s.table, err)
		} else if err = s.checkTable(ctx); err != nil {
			err = fmt.Errorf("created FileMaker log table %q, but: %w", s.table, err)
		} else {
			s.created = true
		}
	}
	if err != nil {
		s.client.CloseIdleConnections()
		return nil, err
	}
	return s, nil
}

// CreatedLogTable reports whether NewFileMaker created the log table.
func (s *FileMakerStore) CreatedLogTable() bool {
	return s.created
}

// checkTable reads no records but every field of the log table, so a missing
// table or field fails.
func (s *FileMakerStore) checkTable(ctx context.Context) error {
	query := url.Values{}
	query.Set("$select", fileMakerSelectFields)
	query.Set("$top", "0")
	if _, err := s.client.GetRecords(ctx, s.table, query); err != nil {
		return fmt.Errorf("check FileMaker log table %q: %w", s.table, err)
	}
	return nil
}

// fileMakerCodeTableMissing is FileMaker's error code for a table that isn't
// defined in the database, sent with 404.
const fileMakerCodeTableMissing = "-1020"

// isMissingTable reports whether err is FileMaker saying the table doesn't
// exist, and not a missing field, a wrong password or a network error.
func isMissingTable(err error) bool {
	var se *fmsodata.StatusError
	if !errors.As(err, &se) || se.StatusCode != http.StatusNotFound {
		return false
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	return json.Unmarshal([]byte(se.Body), &body) == nil && body.Error.Code == fileMakerCodeTableMissing
}

// logTableDefinition is the log table as startup creates it.
func logTableDefinition(name string) fmsodata.TableDefinition {
	return fmsodata.TableDefinition{TableName: name, Fields: logTableFields}
}

// NewFileMakerClient returns an OData client for the FMS_* connection
// settings; the replica (#66) shares them with the log table.
func NewFileMakerClient(cfg types.FileMakerSettings) (*fmsodata.Client, error) {
	tlsConfig, err := fileMakerTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	return fmsodata.NewClient(fmsodata.ClientConfig{
		Host:      strings.TrimSuffix(cfg.Host, "/"),
		Database:  cfg.Database,
		Username:  cfg.Username,
		Password:  cfg.Password,
		Timeout:   cfg.Timeout,
		TLSConfig: tlsConfig,
	}), nil
}

func fileMakerTLSConfig(cfg types.FileMakerSettings) (*tls.Config, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.InsecureSkipVerify}
	if cfg.CAFile == "" {
		return tlsConfig, nil
	}
	pem, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read FMS_CA_FILE: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("FMS_CA_FILE %q contains no PEM certificates", cfg.CAFile)
	}
	tlsConfig.RootCAs = pool
	return tlsConfig, nil
}

func (s *FileMakerStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx)
}

func (s *FileMakerStore) Close() error {
	s.client.CloseIdleConnections()
	return nil
}

// LogRequests creates the batch in one $batch request. ID is left to the
// table's auto-enter serial number.
func (s *FileMakerStore) LogRequests(ctx context.Context, entries []types.UsageLog) error {
	records := make([]map[string]interface{}, len(entries))
	for i, e := range entries {
		records[i] = map[string]interface{}{
			"Status":    e.Status,
			"Method":    e.Method,
			"Error":     e.Error,
			"Endpoint":  e.Endpoint,
			"CreatedAt": e.CreatedAt.UTC().Format(fileMakerTimeLayout),
			"Request":   string(e.Request),
			"Response":  string(e.Response),
		}
	}
	return classifyFileMakerError(s.client.CreateRecords(ctx, s.table, records))
}

// classifyFileMakerError marks client errors that retrying cannot fix (a
// rejected record, bad credentials) as permanent. Timeouts and rate limits
// stay retryable.
func classifyFileMakerError(err error) error {
	var se *fmsodata.StatusError
	if errors.As(err, &se) && se.StatusCode >= 400 && se.StatusCode < 500 &&
		se.StatusCode != http.StatusRequestTimeout && se.StatusCode != http.StatusTooManyRequests {
		return Permanent(err)
	}
	return err
}

func (s *FileMakerStore) GetLogs(ctx context.Context, from, to time.Time, page Page) ([]types.UsageLog, error) {
	query := url.Values{}
	query.Set("$select", fileMakerSelectFields)
	query.Set("$filter", fmt.Sprintf("CreatedAt ge %s and CreatedAt lt %s",
		from.UTC().Format(fileMakerTimeLayout), to.UTC().Format(fileMakerTimeLayout)))
	query.Set("$orderby", "CreatedAt asc,ID asc")
	if page.Limit > 0 {
		query.Set("$top", strconv.Itoa(page.Limit))
	}
	if page.Offset > 0 {
		query.Set("$skip", strconv.Itoa(page.Offset))
	}

	records, err := s.client.GetRecords(ctx, s.table, query)
	if err != nil {
		return nil, err
	}

	var logs []types.UsageLog
	for _, r := range records {
		created, err := parseFileMakerTime(r["CreatedAt"])
		if err != nil {
			return nil, fmt.Errorf("record %v: %w", r["ID"], err)
		}
		logs = append(logs, types.UsageLog{
			ID:        fileMakerInt(r["ID"]),
			Status:    fileMakerInt(r["Status"]),
			Method:    fileMakerString(r["Method"]),
			Error:     fileMakerString(r["Error"]),
			Endpoint:  fileMakerString(r["Endpoint"]),
			CreatedAt: created,
			Request:   types.RawJSON(fileMakerString(r["Request"])),
			Response:  types.RawJSON(fileMakerString(r["Response"])),
		})
	}
	return logs, nil
}

func fileMakerInt(v interface{}) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

func fileMakerString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// parseFileMakerTime reads a timestamp as UTC wall-clock time. CreatedAt is
// written as UTC without an offset, so any offset FileMaker adds on the way
// back (the server's zone) is ignored.
func parseFileMakerTime(v interface{}) (time.Time, error) {
	s := fileMakerString(v)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999"} {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized FileMaker timestamp %q", s)
}
