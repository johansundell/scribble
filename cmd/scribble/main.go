// Command scribble is the command-line client for a scribble server.
//
//	SCRIBBLE_URL    server address (default http://localhost:8080)
//	SCRIBBLE_TOKEN  the server's AUTH_TOKEN
package main

import (
	"context"
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

const usage = `scribble - notes from the command line (version %s)

Usage:
  scribble new <title> [text...]     create a note (text may also be piped in)
  scribble show <title>              print a note's Markdown
  scribble edit <title>              edit in $EDITOR
  scribble append <title> [text...]  add text at the end (creates the note if missing; pipe-friendly)
  scribble ls                        list all notes
  scribble search <text>             find notes containing text
  scribble links <title>             notes this note links to
  scribble backlinks <title>         notes that link to this note
  scribble projects [<name>]         list #projects, or the notes in one
  scribble rename <title> <new>      retitle a note and fix [[links]] to it
  scribble autolink <title>          turn mentions of other note titles into [[links]]
  scribble import <path...>          import Tomboy .note XML files or directories
  scribble rm <title>                delete a note
  scribble completion [bash]         output shell completion script

Environment:
  SCRIBBLE_URL    server address (default http://localhost:8080)
  SCRIBBLE_TOKEN  the server's AUTH_TOKEN
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		if client.IsUnauthorized(err) {
			err = fmt.Errorf("the server rejected SCRIBBLE_TOKEN")
		}
		fmt.Fprintln(os.Stderr, "scribble:", err)
		os.Exit(1)
	}
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
	token := os.Getenv("SCRIBBLE_TOKEN")
	if token == "" {
		return fmt.Errorf("SCRIBBLE_TOKEN is not set")
	}
	baseURL := os.Getenv("SCRIBBLE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	c := client.New(baseURL, token)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "new":
		return cmdNew(ctx, c, rest)
	case "show":
		return withNote(ctx, c, rest, 1, func(n *notes.Note, _ []string) error {
			fmt.Print(n.Body)
			if !strings.HasSuffix(n.Body, "\n") {
				fmt.Println()
			}
			return nil
		})
	case "edit":
		return cmdEdit(ctx, c, rest)
	case "append":
		return cmdAppend(ctx, c, rest)
	case "ls":
		list, err := c.List(ctx, "")
		if err != nil {
			return err
		}
		printList(os.Stdout, list)
		return nil
	case "search":
		if len(rest) == 0 {
			return fmt.Errorf("usage: scribble search <text>")
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
	case "projects":
		return cmdProjects(ctx, c, os.Stdout, rest)
	case "autolink":
		return cmdAutoLink(ctx, c, os.Stdout, rest)
	case "import":
		return cmdImport(ctx, c, os.Stdout, rest)
	case "rm":
		return withNote(ctx, c, rest, 1, func(n *notes.Note, _ []string) error {
			if err := c.Delete(ctx, n.ID); err != nil {
				return err
			}
			fmt.Printf("deleted %q\n", n.Title)
			return nil
		})
	}
	return fmt.Errorf("unknown command %q (try: scribble help)", cmd)
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
	return fmt.Errorf("usage: scribble projects [<name>]")
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

// cmdImport parses Tomboy .note XML files (or directories of .note files) and
// creates each note on the server, skipping titles that already exist.
func cmdImport(ctx context.Context, c *client.Client, w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: scribble import <path...>")
	}
	files, err := collectNoteFiles(args)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no .note files found")
	}
	var created, skipped int
	for _, path := range files {
		body, err := readTomboyFile(path)
		if err != nil {
			return err
		}
		title := notes.TitleOf(body)
		n, err := c.Create(ctx, body)
		if client.IsTitleTaken(err) {
			fmt.Fprintf(w, "skipped %q (already exists)\n", title)
			skipped++
			continue
		}
		if err != nil {
			return explain(err, title)
		}
		fmt.Fprintf(w, "created %q\n", n.Title)
		created++
	}
	fmt.Fprintf(w, "imported %d note(s), skipped %d\n", created, skipped)
	return nil
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
		return fmt.Errorf("usage: scribble new <title> [text...]")
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
		return fmt.Errorf("usage: scribble append <title> [text...]")
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
