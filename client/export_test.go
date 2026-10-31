package client_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/notes"
)

func TestClientExportImport(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	if _, err := c.Create(ctx, "# Ideas\nfirst"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := c.Export(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	list, err := notes.ReadZip(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil || len(list) != 1 || list[0].Body != "# Ideas\nfirst" {
		t.Fatalf("export = %+v, %v", list, err)
	}

	var archive bytes.Buffer
	if err := notes.WriteZip(&archive, []notes.Note{
		{Title: "Ideas", Body: "# Ideas\nsecond"},
		{Title: "Plans", Body: "# Plans\n"},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := c.Import(ctx, bytes.NewReader(archive.Bytes()), false)
	if err != nil || res != (client.ImportResult{Imported: 1, Skipped: 1}) {
		t.Fatalf("Import = %+v, %v", res, err)
	}
	res, err = c.Import(ctx, bytes.NewReader(archive.Bytes()), true)
	if err != nil || res != (client.ImportResult{Imported: 1, Skipped: 1}) {
		t.Fatalf("Import overwrite = %+v, %v", res, err)
	}
	if n, _ := c.ByTitle(ctx, "Ideas"); n.Body != "# Ideas\nsecond" {
		t.Errorf("overwrite left %q", n.Body)
	}
	if _, err := c.Import(ctx, bytes.NewReader([]byte("nope")), false); err == nil {
		t.Error("want an error for a bad archive")
	}
}
