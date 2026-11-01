// Command sn is the command-line client for a scribble server.
//
//	SCRIBBLE_URL    server address (default https://notes.sudde.net)
//	SCRIBBLE_TOKEN  the server's AUTH_TOKEN
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/notes"
)

var Version = "dev"

// installCmd installs or upgrades sn from the latest release (#130).
const installCmd = "curl -fsSL https://raw.githubusercontent.com/johansundell/scribble/master/scripts/install-sn.sh | sh"

// installHint is prefix and installCmd on the systems the installer runs on
// (Linux and macOS), and nothing elsewhere.
func installHint(goos, prefix string) string {
	if goos == "linux" || goos == "darwin" {
		return prefix + installCmd
	}
	return ""
}

const usage = `sn - notes from the command line (version %s)

Usage:
  sn new <title> [text...]     create a note (text may also be piped in)
  sn show <title> [-r <rev>]   print a note's Markdown, or that of an old version
  sn edit <title>              edit in $EDITOR
  sn append <title> [text...]  add text at the end (creates the note if missing; pipe-friendly)
  sn ls                        list all notes, pinned ones first (📌)
  sn search <text>             find notes containing text
  sn links <title>             notes this note links to
  sn backlinks <title>         notes that link to this note
  sn projects [<name>]         list #projects, or the notes in one
  sn pin <title>               keep a note at the top of lists
  sn unpin <title>             let it sort by date again
  sn rename <title> <new>      retitle a note and fix [[links]] to it
  sn autolink <title>          turn mentions of other note titles into [[links]]
  sn export [file.zip]         save every note as <title>.md in a ZIP archive
                               (default scribble-notes-YYYY-MM-DD.zip)
  sn import [--overwrite] <path...>
                               import ZIP archives of .md/.txt/.note files, and
                               Tomboy .note files or directories; existing
                               titles are skipped unless --overwrite
  sn rm <title>                delete a note
  sn history <title>           list a note's versions, newest first
  sn diff <title> [<rev> [<rev2>]]
                               show what changed between two versions
  sn restore [-y] <title> <rev>
                               make an old version the current text
  sn deleted                   list deleted notes
  sn undelete <title>          bring back a deleted note
  sn completion [bash]         output shell completion script
  sn version                   print the client's and the server's versions

Examples:
  sn history "Work log"        see its versions
  sn show "Work log" -r 3      print version r3
  sn diff "Work log"           what the last change did
  sn diff "Work log" 3 5       what changed from r3 to r5
  sn restore "Work log" r3     make r3 the current text again
  sn deleted                   then: sn undelete "Work log"

Environment:
  SCRIBBLE_URL    server address (default https://notes.sudde.net)
  SCRIBBLE_TOKEN  the server's AUTH_TOKEN
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, errorLine(err))
		os.Exit(1)
	}
}

// errorLine is what main prints for err.
func errorLine(err error) string {
	switch {
	case client.IsUnauthorized(err):
		err = fmt.Errorf("the server rejected SCRIBBLE_TOKEN")
	case client.IsUpgradeRequired(err):
		err = fmt.Errorf("this version (%s) is too old for the server; download the latest from %s%s",
			Version, client.ReleasesURL, installHint(runtime.GOOS, " or run: "))
	}
	return "sn: " + err.Error()
}

func run(args []string) error {
	return runWith(os.Stdout, args)
}

func runWith(stdout io.Writer, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintf(stdout, usage, Version)
		return nil
	}
	if args[0] == "completion" {
		return cmdCompletion(stdout, args[1:])
	}
	baseURL := client.ServerURL()
	token := os.Getenv("SCRIBBLE_TOKEN")
	c := client.New(baseURL, token)
	c.Name, c.Version = "cli", Version
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if args[0] == "version" {
		return cmdVersion(ctx, c, stdout) // /healthz needs no token
	}
	if token == "" {
		return fmt.Errorf("SCRIBBLE_TOKEN is not set")
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "new":
		return cmdNew(ctx, c, rest)
	case "show":
		return cmdShow(ctx, c, stdout, rest)
	case "edit":
		return cmdEdit(ctx, c, rest)
	case "append":
		return cmdAppend(ctx, c, rest)
	case "ls":
		return cmdLs(ctx, c, stdout)
	case "search":
		if len(rest) == 0 {
			return fmt.Errorf("usage: sn search <text>")
		}
		list, err := c.List(ctx, strings.Join(rest, " "))
		if err != nil {
			return err
		}
		printList(os.Stdout, list)
		return nil
	case "links":
		return withNote(ctx, c, rest, 1, func(n *notes.Note, _ []string) error {
			for _, l := range notes.Links(n.Body) {
				fmt.Println(l)
			}
			return nil
		})
	case "backlinks":
		return withNote(ctx, c, rest, 1, func(n *notes.Note, _ []string) error {
			list, err := c.Backlinks(ctx, n.ID)
			if err != nil {
				return err
			}
			printList(os.Stdout, list)
			return nil
		})
	case "rename":
		return withNote(ctx, c, rest, 2, func(n *notes.Note, a []string) error {
			r, err := c.Rename(ctx, n.ID, a[1])
			if err != nil {
				return explain(err, a[1])
			}
			fmt.Printf("renamed to %q\n", r.Title)
			return nil
		})
	case "pin", "unpin":
		return cmdPin(ctx, c, stdout, cmd == "pin", rest)
	case "projects":
		return cmdProjects(ctx, c, os.Stdout, rest)
	case "autolink":
		return cmdAutoLink(ctx, c, os.Stdout, rest)
	case "export":
		return cmdExport(ctx, c, stdout, rest, time.Now())
	case "import":
		return cmdImport(ctx, c, stdout, rest)
	case "rm":
		return withNote(ctx, c, rest, 1, func(n *notes.Note, _ []string) error {
			if err := c.Delete(ctx, n.ID); err != nil {
				return err
			}
			fmt.Printf("deleted %q\n", n.Title)
			return nil
		})
	case "history":
		return cmdHistory(ctx, c, stdout, rest)
	case "diff":
		return cmdDiff(ctx, c, stdout, diffColour(), rest)
	case "restore":
		return cmdRestore(ctx, c, stdout, os.Stdin, !stdinPiped(), rest)
	case "deleted":
		return cmdDeleted(ctx, c, stdout, rest)
	case "undelete":
		return cmdUndelete(ctx, c, stdout, rest)
	}
	return fmt.Errorf("unknown command %q (try: sn help)", cmd)
}

// cmdVersion prints the client's and the server's versions and API levels,
// and says when the server runs a newer release. Only this command says so,
// so scripts using the others aren't affected.
func cmdVersion(ctx context.Context, c *client.Client, w io.Writer) error {
	fmt.Fprintf(w, "client %s (API level %d)\n", Version, client.APILevel)
	h, err := c.Health(ctx)
	if err != nil {
		return fmt.Errorf("cannot ask the server for its version: %w", err)
	}
	schema := ""
	if h.Schema > 0 {
		schema = fmt.Sprintf(", schema %d", h.Schema)
	}
	fmt.Fprintf(w, "server %s (API level %d%s)\n", h.Version, h.API, schema)
	if client.NewerRelease(Version, h.Version) {
		fmt.Fprintf(w, "update available: %s\n", client.ReleasesURL)
		if h := installHint(runtime.GOOS, "run: "); h != "" {
			fmt.Fprintln(w, h)
		}
	}
	return nil
}

// withNote looks up the note named by args[0] and calls fn. want is the
// exact number of arguments the command takes.
func withNote(ctx context.Context, c *client.Client, args []string, want int, fn func(*notes.Note, []string) error) error {
	if len(args) != want {
		return fmt.Errorf("expected %d argument(s); quote titles that contain spaces", want)
	}
	n, err := c.ByTitle(ctx, args[0])
	if err != nil {
		return explain(err, args[0])
	}
	return fn(n, args)
}

// parseFlags parses args with fs, allowing flags before, between and after
// the positional arguments, which it returns. Everything after -- is
// positional, for titles that start with -.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if used := len(args) - fs.NArg(); used > 0 && args[used-1] == "--" {
			return append(pos, fs.Args()...), nil
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func explain(err error, title string) error {
	switch {
	case client.IsNotFound(err):
		return fmt.Errorf("no note titled %q", title)
	case client.IsTitleTaken(err):
		return fmt.Errorf("a note titled %q already exists", title)
	case client.IsUnauthorized(err):
		return fmt.Errorf("the server rejected SCRIBBLE_TOKEN")
	case client.IsStale(err):
		return fmt.Errorf("the note was changed elsewhere since you started")
	}
	return err
}

func printList(w io.Writer, list []notes.Note) {
	for _, n := range list {
		fmt.Fprintln(w, n.Title)
	}
}

// pinMark comes before a pinned note's title in sn ls (#140).
const pinMark = "📌 "

// cmdLs prints every title, pinned notes first and marked pinMark. Only ls
// marks them, so the other lists stay plain titles for pipes.
func cmdLs(ctx context.Context, c *client.Client, w io.Writer) error {
	list, err := c.List(ctx, "")
	if err != nil {
		return err
	}
	for _, n := range list {
		if n.Pinned {
			fmt.Fprint(w, pinMark)
		}
		fmt.Fprintln(w, n.Title)
	}
	return nil
}

// cmdPin pins or unpins the note titled args[0] (#140).
func cmdPin(ctx context.Context, c *client.Client, w io.Writer, pin bool, args []string) error {
	return withNote(ctx, c, args, 1, func(n *notes.Note, _ []string) error {
		p, err := c.Pin(ctx, n.ID, pin)
		if err != nil {
			return explain(err, n.Title)
		}
		verb := "pinned"
		if !pin {
			verb = "unpinned"
		}
		fmt.Fprintf(w, "%s %q\n", verb, p.Title)
		return nil
	})
}

// cmdProjects prints every #project, or with one argument the titles of the
// notes in that project.
func cmdProjects(ctx context.Context, c *client.Client, w io.Writer, args []string) error {
	switch len(args) {
	case 0:
		list, err := c.Projects(ctx)
		if err != nil {
			return err
		}
		for _, p := range list {
			fmt.Fprintln(w, p)
		}
		return nil
	case 1:
		list, err := c.ListInProject(ctx, args[0], "")
		if err != nil {
			return err
		}
		printList(w, list)
		return nil
	}
	return fmt.Errorf("usage: sn projects [<name>]")
}

// cmdAutoLink turns bare mentions of other notes' titles in one note into
// [[links]] and saves it, for users who ask for it.
func cmdAutoLink(ctx context.Context, c *client.Client, w io.Writer, args []string) error {
	return withNote(ctx, c, args, 1, func(n *notes.Note, _ []string) error {
		list, err := c.List(ctx, "")
		if err != nil {
			return err
		}
		titles := make([]string, len(list))
		for i, other := range list {
			titles[i] = other.Title
		}
		body, linked := notes.AutoLink(n.Body, titles)
		if linked == 0 {
			fmt.Fprintln(w, "no mentions to link")
			return nil
		}
		if _, err := c.Update(ctx, n.ID, body, n.Revision); err != nil {
			return explain(err, n.Title)
		}
		fmt.Fprintf(w, "linked %d mention(s) in %q\n", linked, n.Title)
		return nil
	})
}

// cmdExport saves every note to a ZIP archive: args[0], or
// scribble-notes-YYYY-MM-DD.zip for day now in the current directory.
func cmdExport(ctx context.Context, c *client.Client, w io.Writer, args []string, now time.Time) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: sn export [file.zip]")
	}
	path := notes.ZipFileName(now)
	if len(args) == 1 {
		path = args[0]
	}
	// Written beside path and renamed, so a failed export leaves a file
	// already at path as it was.
	f, err := os.CreateTemp(filepath.Dir(path), ".scribble-export-*.zip")
	if err != nil {
		return err
	}
	err = c.Export(ctx, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
		return err
	}
	fmt.Fprintf(w, "exported to %s\n", path)
	return nil
}

// cmdImport imports ZIP archives through the server, and parses Tomboy
// .note XML files (or directories of .note files) and creates each note.
// Titles that already exist are skipped, or with --overwrite replaced.
func cmdImport(ctx context.Context, c *client.Client, w io.Writer, args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	overwrite := fs.Bool("overwrite", false, "replace notes that already exist")
	args, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: sn import [--overwrite] <path...>")
	}
	var archives, others []string
	for _, p := range args {
		if strings.EqualFold(filepath.Ext(p), ".zip") {
			archives = append(archives, p)
		} else {
			others = append(others, p)
		}
	}
	files, err := collectNoteFiles(others)
	if err != nil {
		return err
	}
	if len(files) == 0 && len(archives) == 0 {
		return fmt.Errorf("no .note or .zip files found")
	}
	var total client.ImportResult
	for _, path := range archives {
		res, err := importZip(ctx, c, path, *overwrite)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		total.Imported += res.Imported
		total.Skipped += res.Skipped
	}
	for _, path := range files {
		body, err := readTomboyFile(path)
		if err != nil {
			return err
		}
		title := notes.TitleOf(body)
		n, err := c.Create(ctx, body)
		if client.IsTitleTaken(err) && *overwrite {
			var changed bool
			if n, changed, err = overwriteNote(ctx, c, title, body); err == nil {
				if changed {
					fmt.Fprintf(w, "overwrote %q\n", n.Title)
					total.Imported++
				} else {
					fmt.Fprintf(w, "skipped %q (unchanged)\n", n.Title)
					total.Skipped++
				}
				continue
			}
		}
		if client.IsTitleTaken(err) {
			fmt.Fprintf(w, "skipped %q (already exists)\n", title)
			total.Skipped++
			continue
		}
		if err != nil {
			return explain(err, title)
		}
		fmt.Fprintf(w, "created %q\n", n.Title)
		total.Imported++
	}
	fmt.Fprintf(w, "imported %d note(s), skipped %d\n", total.Imported, total.Skipped)
	return nil
}

// importZip checks that path is a ZIP of notes, so a bad one gets a clear
// message, and uploads it.
func importZip(ctx context.Context, c *client.Client, path string, overwrite bool) (client.ImportResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return client.ImportResult{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return client.ImportResult{}, err
	}
	if _, err := notes.ReadZip(f, info.Size()); err != nil {
		return client.ImportResult{}, err
	}
	return c.Import(ctx, io.NewSectionReader(f, 0, info.Size()), overwrite)
}

// overwriteNote replaces the text of the note titled title with body, as
// the server's import does: a note that has it already is left alone, and
// changed is false.
func overwriteNote(ctx context.Context, c *client.Client, title, body string) (n *notes.Note, changed bool, err error) {
	cur, err := c.ByTitle(ctx, title)
	if err != nil || cur.Body == body {
		return cur, false, err
	}
	n, err = c.Update(ctx, cur.ID, body, cur.Revision)
	return n, err == nil, err
}

func collectNoteFiles(paths []string) ([]string, error) {
	var files []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, p)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".note") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func readTomboyFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	body, err := notes.ParseTomboy(f)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return body, nil
}

func stdinPiped() bool {
	st, err := os.Stdin.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice == 0
}

// textFrom returns the args joined as text, or stdin when piped and no args.
func textFrom(args []string) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	if stdinPiped() {
		b, err := io.ReadAll(os.Stdin)
		return strings.TrimRight(string(b), "\n"), err
	}
	return "", nil
}

func newBody(title, text string) string {
	body := "# " + title + "\n"
	if text != "" {
		body += "\n" + text + "\n"
	}
	return body
}

func cmdNew(ctx context.Context, c *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: sn new <title> [text...]")
	}
	title := args[0]
	text, err := textFrom(args[1:])
	if err != nil {
		return err
	}
	n, err := c.Create(ctx, newBody(title, text))
	if err != nil {
		return explain(err, title)
	}
	fmt.Printf("created %q\n", n.Title)
	return nil
}

func cmdAppend(ctx context.Context, c *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: sn append <title> [text...]")
	}
	title := args[0]
	text, err := textFrom(args[1:])
	if err != nil {
		return err
	}
	if text == "" {
		return fmt.Errorf("nothing to append (give text or pipe it in)")
	}
	// Re-read and retry if someone else saves between our read and write.
	for attempt := 0; attempt < 3; attempt++ {
		n, err := c.ByTitle(ctx, title)
		if client.IsNotFound(err) {
			if _, err := c.Create(ctx, newBody(title, text)); err != nil {
				if client.IsTitleTaken(err) {
					continue // created concurrently; append on the next pass
				}
				return explain(err, title)
			}
			fmt.Printf("created %q\n", title)
			return nil
		}
		if err != nil {
			return explain(err, title)
		}
		body := strings.TrimRight(n.Body, "\n") + "\n\n" + text + "\n"
		if _, err := c.Update(ctx, n.ID, body, n.Revision); err != nil {
			if client.IsStale(err) {
				continue
			}
			return explain(err, title)
		}
		return nil
	}
	return fmt.Errorf("gave up: %q keeps changing under us", title)
}

func editorCommand() string {
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

func cmdEdit(ctx context.Context, c *client.Client, args []string) error {
	return withNote(ctx, c, args, 1, func(n *notes.Note, _ []string) error {
		f, err := os.CreateTemp("", "scribble-*.md")
		if err != nil {
			return err
		}
		path := f.Name()
		if _, err := f.WriteString(n.Body); err != nil {
			f.Close()
			return err
		}
		f.Close()

		parts := strings.Fields(editorCommand())
		cmd := exec.Command(parts[0], append(parts[1:], path)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			os.Remove(path)
			return fmt.Errorf("editor failed: %w", err)
		}
		edited, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(edited) == n.Body {
			os.Remove(path)
			fmt.Println("no changes")
			return nil
		}
		if _, err := c.Update(ctx, n.ID, string(edited), n.Revision); err != nil {
			// Never lose the user's work: keep the temp file and say where it is.
			return fmt.Errorf("%w (your edit is saved in %s)", explain(err, n.Title), path)
		}
		os.Remove(path)
		fmt.Printf("saved %q\n", notes.TitleOf(string(edited)))
		return nil
	})
}
