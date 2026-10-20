package replica

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/johansundell/scribble/fmsodata"
)

// fakeFM is an in-memory FileMaker OData API. Like FileMaker, it stores
// line breaks in text as CR, and answers 404 for a missing record.
type fakeFM struct {
	mu     sync.Mutex
	fields map[string][]string                              // table → its fields; the first is the key
	tables map[string]map[int64]map[string]any              // table → key → record
	fail   func(op, table string, rec map[string]any) error // nil, or the error for this call
	calls  []string
}

func newFakeFM() *fakeFM {
	f := &fakeFM{fields: map[string][]string{
		"Notes":        noteFields,
		"PendingEdits": pendingFields,
		"ScribbleInfo": infoFields,
	}, tables: map[string]map[int64]map[string]any{}}
	for t := range f.fields {
		f.tables[t] = map[int64]map[string]any{}
	}
	f.tables["ScribbleInfo"][1] = map[string]any{"SchemaVersion": float64(SchemaVersion)}
	return f
}

func notFound() error { return &fmsodata.StatusError{StatusCode: http.StatusNotFound} }

func (f *fakeFM) check(op, table string, rec map[string]any) error {
	f.calls = append(f.calls, op+" "+table)
	if f.fail != nil {
		if err := f.fail(op, table, rec); err != nil {
			return err
		}
	}
	if _, ok := f.fields[table]; !ok {
		return notFound()
	}
	for k := range rec {
		if !slices.Contains(f.fields[table], k) {
			return &fmsodata.StatusError{StatusCode: http.StatusBadRequest, Body: "no field " + k}
		}
	}
	return nil
}

// stored converts a record as FileMaker would: numbers become float64 and
// LF becomes CR.
func stored(rec map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range rec {
		switch v := v.(type) {
		case int64:
			out[k] = float64(v)
		case int:
			out[k] = float64(v)
		case string:
			out[k] = strings.ReplaceAll(strings.ReplaceAll(v, "\r\n", "\r"), "\n", "\r")
		default:
			out[k] = v
		}
	}
	return out
}

func (f *fakeFM) GetRecords(ctx context.Context, table string, q url.Values) ([]map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sel := map[string]any{}
	if s := q.Get("$select"); s != "" {
		for _, k := range strings.Split(s, ",") {
			sel[k] = nil
		}
	}
	if err := f.check("GET", table, sel); err != nil {
		return nil, err
	}
	keys := []int64{}
	for k := range f.tables[table] {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	skip, _ := strconv.Atoi(q.Get("$skip"))
	keys = keys[min(skip, len(keys)):]
	if top := q.Get("$top"); top != "" {
		n, _ := strconv.Atoi(top)
		keys = keys[:min(n, len(keys))]
	}
	out := []map[string]any{}
	for _, k := range keys {
		r := map[string]any{}
		for field, v := range f.tables[table][k] {
			r[field] = v
		}
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeFM) CreateRecord(ctx context.Context, table string, data map[string]any) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check("POST", table, data); err != nil {
		return nil, err
	}
	rec := stored(data)
	key := int64(rec[f.fields[table][0]].(float64))
	if _, ok := f.tables[table][key]; ok {
		return nil, &fmsodata.StatusError{StatusCode: http.StatusBadRequest, Body: "duplicate key"}
	}
	f.tables[table][key] = rec
	return rec, nil
}

func (f *fakeFM) UpdateRecordByKey(ctx context.Context, table, key string, data map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check("PATCH", table, data); err != nil {
		return err
	}
	k, _ := strconv.ParseInt(key, 10, 64)
	rec, ok := f.tables[table][k]
	if !ok {
		return notFound()
	}
	for field, v := range stored(data) {
		rec[field] = v
	}
	return nil
}

func (f *fakeFM) DeleteRecordByKey(ctx context.Context, table, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check("DELETE", table, nil); err != nil {
		return err
	}
	k, _ := strconv.ParseInt(key, 10, 64)
	if _, ok := f.tables[table][k]; !ok {
		return notFound()
	}
	delete(f.tables[table], k)
	return nil
}

// record returns a copy of table's record key, or nil.
func (f *fakeFM) record(table string, key int64) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.tables[table][key]
	if !ok {
		return nil
	}
	out := map[string]any{}
	for k, v := range r {
		out[k] = v
	}
	return out
}

// queue adds a PendingEdits row, as FileMaker (#67) would.
func (f *fakeFM) queue(id int64, kind string, noteID int64, body string, baseRev int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec := stored(map[string]any{"ID": id, "Kind": kind, "Body": body, "BaseRevision": baseRev, "Status": "", "Message": ""})
	if noteID != 0 {
		rec["NoteID"] = float64(noteID)
	}
	f.tables["PendingEdits"][id] = rec
}

func (f *fakeFM) count(table string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tables[table])
}

func errOn(op, table string) func(string, string, map[string]any) error {
	return func(o, t string, _ map[string]any) error {
		if o == op && t == table {
			return fmt.Errorf("connection refused")
		}
		return nil
	}
}
