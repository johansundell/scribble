// Command winicon puts every size of an .ico into a Windows exe built by
// fyne-cross (#123). Fyne's packager keeps only the largest image of its
// -icon, so make compile runs this afterwards:
//
//	go run ./cmd/winicon -ico cmd/scribble-gui/icon.ico scribble-gui.exe
//
// It replaces the exe's icon frames and every icon group with the .ico,
// stored once and shared by the groups, and leaves the other resources
// (version, manifest) as they are.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/tc-hib/winres"
)

func main() {
	ico := flag.String("ico", "", "the .ico to embed")
	flag.Parse()
	if *ico == "" || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: winicon -ico icon.ico app.exe")
		os.Exit(2)
	}
	if err := patch(flag.Arg(0), *ico); err != nil {
		fmt.Fprintln(os.Stderr, "winicon:", err)
		os.Exit(1)
	}
}

// patch rewrites the exe at exePath with the icon at icoPath.
func patch(exePath, icoPath string) error {
	f, err := os.Open(icoPath)
	if err != nil {
		return err
	}
	icon, err := winres.LoadICO(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("%s: %w", icoPath, err)
	}

	in, err := os.Open(exePath)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	rs, err := winres.LoadFromEXE(in)
	if err != nil {
		return fmt.Errorf("%s: %w", exePath, err)
	}
	if err := replaceIcons(rs, icon); err != nil {
		return fmt.Errorf("%s: %w", exePath, err)
	}

	tmp := exePath + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer os.Remove(tmp) // a no-op once renamed
	if err := rs.WriteToEXE(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	in.Close()
	return os.Rename(tmp, exePath)
}

// res names one resource of a type: an ID in one language.
type res struct {
	id   winres.Identifier
	lang uint16
}

// all lists the resources of a type.
func all(rs *winres.ResourceSet, typeID winres.Identifier) []res {
	var out []res
	rs.WalkType(typeID, func(id winres.Identifier, lang uint16, _ []byte) bool {
		out = append(out, res{id, lang})
		return true
	})
	return out
}

// replaceIcons swaps every icon group in rs for icon, keeping each group's ID
// and language. The old frames are deleted, and the new ones are stored once.
func replaceIcons(rs *winres.ResourceSet, icon *winres.Icon) error {
	groups := all(rs, winres.RT_GROUP_ICON)
	if len(groups) == 0 {
		return errors.New("no icon groups to replace")
	}
	// Setting nil deletes.
	for _, r := range all(rs, winres.RT_ICON) {
		if err := rs.Set(winres.RT_ICON, r.id, r.lang, nil); err != nil {
			return err
		}
	}
	for _, g := range groups {
		if err := rs.Set(winres.RT_GROUP_ICON, g.id, g.lang, nil); err != nil {
			return err
		}
	}

	first := groups[0]
	if err := rs.SetIconTranslation(first.id, first.lang, icon); err != nil {
		return err
	}
	dir := rs.Get(winres.RT_GROUP_ICON, first.id, first.lang)
	for _, g := range groups[1:] {
		if err := rs.Set(winres.RT_GROUP_ICON, g.id, g.lang, bytes.Clone(dir)); err != nil {
			return err
		}
	}
	return nil
}
