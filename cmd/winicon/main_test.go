package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tc-hib/winres"
)

// fyneResources is what Fyne's Windows packager writes (#123): one
// single-size icon under groups #2 and IDI_APPLICATION (#32512), a version
// and a manifest.
func fyneResources(t *testing.T) *winres.ResourceSet {
	t.Helper()
	single, err := winres.NewIconFromImages([]image.Image{image.NewNRGBA(image.Rect(0, 0, 256, 256))})
	if err != nil {
		t.Fatal(err)
	}
	rs := &winres.ResourceSet{}
	for _, id := range []winres.Identifier{winres.ID(2), winres.ID(32512)} {
		if err := rs.SetIconTranslation(id, 0x0409, single); err != nil {
			t.Fatal(err)
		}
	}
	if err := rs.Set(winres.RT_VERSION, winres.ID(1), 0x0409, []byte("version")); err != nil {
		t.Fatal(err)
	}
	if err := rs.Set(winres.RT_MANIFEST, winres.ID(1), 0x0409, []byte("<assembly/>")); err != nil {
		t.Fatal(err)
	}
	return rs
}

func appIcon(t *testing.T) *winres.Icon {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "scribble-gui", "icon.ico"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	icon, err := winres.LoadICO(f)
	if err != nil {
		t.Fatal(err)
	}
	return icon
}

// groupFrames reads an RT_GROUP_ICON: a 6-byte header holding the image
// count, then a 14-byte entry per image that starts with its width and ends
// with the RT_ICON ID. A width of 0 means 256.
func groupFrames(t *testing.T, data []byte) (sizes []int, ids []uint16) {
	t.Helper()
	n := int(binary.LittleEndian.Uint16(data[4:6]))
	if len(data) != 6+14*n {
		t.Fatalf("icon group is %d bytes for %d images", len(data), n)
	}
	for i := range n {
		e := data[6+14*i:]
		w := int(e[0])
		if w == 0 {
			w = 256
		}
		sizes = append(sizes, w)
		ids = append(ids, binary.LittleEndian.Uint16(e[12:14]))
	}
	slices.Sort(sizes)
	return sizes, ids
}

func TestReplaceIconsLeavesOneSetOfFramesSharedByBothGroups(t *testing.T) {
	rs := fyneResources(t)

	if err := replaceIcons(rs, appIcon(t)); err != nil {
		t.Fatal(err)
	}

	frames := map[uint16]bool{}
	rs.WalkType(winres.RT_ICON, func(id winres.Identifier, _ uint16, _ []byte) bool {
		frames[uint16(id.(winres.ID))] = true
		return true
	})
	groups := map[resKey][]byte{}
	rs.WalkType(winres.RT_GROUP_ICON, func(id winres.Identifier, lang uint16, data []byte) bool {
		groups[resKey{id, lang}] = data
		return true
	})

	if len(groups) != 2 {
		t.Fatalf("icon groups = %v, want #2 and #32512 in 0409", groups)
	}
	first := groups[resKey{winres.ID(2), 0x0409}]
	if first == nil {
		t.Fatal("no icon group #2 in 0409")
	}
	if !bytes.Equal(groups[resKey{winres.ID(32512), 0x0409}], first) {
		t.Error("icon group #32512 in 0409 differs from #2")
	}
	sizes, ids := groupFrames(t, first)
	if want := []int{16, 24, 32, 48, 64, 128, 256}; !slices.Equal(sizes, want) {
		t.Errorf("icon group sizes = %v, want %v", sizes, want)
	}
	if len(frames) != len(ids) {
		t.Errorf("exe has %d icon frames, the group uses %d; want no leftovers", len(frames), len(ids))
	}
	for _, id := range ids {
		if !frames[id] {
			t.Errorf("icon group points at missing frame %d", id)
		}
	}
}

func TestReplaceIconsKeepsVersionAndManifest(t *testing.T) {
	rs := fyneResources(t)

	if err := replaceIcons(rs, appIcon(t)); err != nil {
		t.Fatal(err)
	}

	if got := rs.Get(winres.RT_VERSION, winres.ID(1), 0x0409); string(got) != "version" {
		t.Errorf("RT_VERSION = %q, want it unchanged", got)
	}
	if got := rs.Get(winres.RT_MANIFEST, winres.ID(1), 0x0409); string(got) != "<assembly/>" {
		t.Errorf("RT_MANIFEST = %q, want it unchanged", got)
	}
}

func TestReplaceIconsWithoutIconGroupsFails(t *testing.T) {
	rs := &winres.ResourceSet{}
	if err := rs.Set(winres.RT_VERSION, winres.ID(1), 0x0409, []byte("version")); err != nil {
		t.Fatal(err)
	}

	if err := replaceIcons(rs, appIcon(t)); err == nil {
		t.Error("an exe without icon groups: got no error")
	}
}
