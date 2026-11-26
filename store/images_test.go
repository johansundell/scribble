package store

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/johansundell/scribble/images"
)

func testImage(id string) images.Image {
	return images.Image{ID: strings.Repeat(id, 64), MIME: "image/png", Data: []byte("png " + id)}
}

func TestImages_PutGetList(t *testing.T) {
	ctx := context.Background()
	s := newNoteStore(t)
	a, b := testImage("a"), testImage("b")

	if _, err := s.GetImage(ctx, a.ID); !errors.Is(err, ErrImageNotFound) {
		t.Fatalf("GetImage before put: %v, want ErrImageNotFound", err)
	}
	for _, img := range []images.Image{b, a, a} { // the same image twice is stored once
		if err := s.PutImage(ctx, img); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.GetImage(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != a.ID || got.MIME != a.MIME || !bytes.Equal(got.Data, a.Data) {
		t.Errorf("GetImage = %+v, want %+v", got, a)
	}

	list, err := s.ListImages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
		t.Fatalf("ListImages = %+v, want a then b", list)
	}
	if list[0].MIME != "image/png" || list[0].Data != nil {
		t.Errorf("ListImages item = %+v, want the type and no bytes", list[0])
	}
}
