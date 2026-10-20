package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/johansundell/scribble/client"
	"github.com/johansundell/scribble/notes"
)

// timeLayout is how history and deleted print times, in local time.
const timeLayout = "2006-01-02 15:04"

// parseFlags parses args with fs, allowing flags before, between and after
// the positional arguments, which it returns.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// noRevision is the error for a revision note title does not have.
func noRevision(title string, rev int64) error {
	arg := title
	if strings.ContainsAny(title, " \t'\"$`\\!*?&;|<>()[]{}#~") {
		arg = fmt.Sprintf("%q", title)
	}
	return fmt.Errorf("no revision r%d of %q (see: scribble history %s)", rev, title, arg)
}

// getVersion returns note n's version at rev, explaining a missing one.
func getVersion(ctx context.Context, c *client.Client, n *notes.Note, rev int64) (*notes.Version, error) {
	v, err := c.GetVersion(ctx, n.ID, rev)
	if client.IsNotFound(err) {
		return nil, noRevision(n.Title, rev)
	}
	return v, err
}

func printBody(w io.Writer, body string) {
	fmt.Fprint(w, body)
	if !strings.HasSuffix(body, "\n") {
		fmt.Fprintln(w)
	}
}

// cmdShow prints a note's Markdown, or with -r that of one of its versions.
func cmdShow(ctx context.Context, c *client.Client, w io.Writer, args []string) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	r := fs.String("r", "", "revision to print")
	args, err := parseFlags(fs, args)
	if err != nil {
		return fmt.Errorf("%v (usage: scribble show <title> [-r <rev>])", err)
	}
	return withNote(ctx, c, args, 1, func(n *notes.Note, _ []string) error {
		if *r == "" {
			printBody(w, n.Body)
			return nil
		}
		rev, err := parseRevision(*r)
		if err != nil {
			return err
		}
		v, err := getVersion(ctx, c, n, rev)
		if err != nil {
			return err
		}
		printBody(w, v.Body)
		return nil
	})
}

// cmdHistory lists a note's versions, newest first, marking the current one.
func cmdHistory(ctx context.Context, c *client.Client, w io.Writer, args []string) error {
	return withNote(ctx, c, args, 1, func(n *notes.Note, _ []string) error {
		list, err := c.Versions(ctx, n.ID)
		if err != nil {
			return err
		}
		for _, v := range list {
			line := fmt.Sprintf("r%d  %s  %s  %s", v.Revision, v.UpdatedAt.Local().Format(timeLayout), v.Reason, v.Title)
			if v.Revision == n.Revision {
				line += "  (current)"
			}
			fmt.Fprintln(w, line)
		}
		return nil
	})
}

// diffColour reports whether diffs get colour: only on a terminal, and
// never with NO_COLOR set.
func diffColour() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	st, err := os.Stdout.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// cmdDiff prints a unified diff between two versions of a note: the one
// before the current against the current, one revision against the
// current, or two revisions.
func cmdDiff(ctx context.Context, c *client.Client, w io.Writer, colour bool, args []string) error {
	if len(args) < 1 || len(args) > 3 {
		return fmt.Errorf("usage: scribble diff <title> [<rev> [<rev2>]]")
	}
	revs := make([]int64, len(args)-1)
	for i, a := range args[1:] {
		rev, err := parseRevision(a)
		if err != nil {
			return err
		}
		revs[i] = rev
	}
	return withNote(ctx, c, args[:1], 1, func(n *notes.Note, _ []string) error {
		if len(revs) == 0 {
			list, err := c.Versions(ctx, n.ID)
			if err != nil {
				return err
			}
			for _, v := range list {
				if v.Revision < n.Revision {
					revs = append(revs, v.Revision)
					break
				}
			}
			if len(revs) == 0 {
				return fmt.Errorf("%q has no earlier version", n.Title)
			}
		}
		versions := make([]*notes.Version, 2)
		versions[1] = &notes.Version{Revision: n.Revision, Title: n.Title, Body: n.Body}
		for i, rev := range revs {
			v, err := getVersion(ctx, c, n, rev)
			if err != nil {
				return err
			}
			versions[i] = v
		}
		a, b := versions[0], versions[1]
		fmt.Fprint(w, unifiedDiff(fmt.Sprintf("%s r%d", a.Title, a.Revision), fmt.Sprintf("%s r%d", b.Title, b.Revision), a.Body, b.Body, colour))
		return nil
	})
}

// cmdRestore makes an old version of a note its text again. When ask is
// set (stdin is a terminal) and -y is not given, it asks first.
func cmdRestore(ctx context.Context, c *client.Client, w io.Writer, in io.Reader, ask bool, args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	yes := fs.Bool("y", false, "restore without asking")
	args, err := parseFlags(fs, args)
	if err != nil || len(args) != 2 {
		return fmt.Errorf("usage: scribble restore [-y] <title> <rev>")
	}
	rev, err := parseRevision(args[1])
	if err != nil {
		return err
	}
	// n.Revision is sent as the base, so a save made from here on is refused.
	return withNote(ctx, c, args, 2, func(n *notes.Note, _ []string) error {
		v, err := getVersion(ctx, c, n, rev)
		if err != nil {
			return err
		}
		if ask && !*yes {
			fmt.Fprintf(w, "Restore %q to r%d? [y/N] ", n.Title, rev)
			answer, _ := bufio.NewReader(in).ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
				fmt.Fprintln(w, "not restored")
				return nil
			}
		}
		if _, err := c.Restore(ctx, n.ID, rev, n.Revision); err != nil {
			if client.IsNotFound(err) {
				return noRevision(n.Title, rev)
			}
			return explain(err, v.Title)
		}
		fmt.Fprintf(w, "restored %q to r%d\n", n.Title, rev)
		return nil
	})
}

// cmdDeleted lists the deleted notes, most recently deleted first.
func cmdDeleted(ctx context.Context, c *client.Client, w io.Writer, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: scribble deleted")
	}
	list, err := c.Deleted(ctx)
	if err != nil {
		return err
	}
	for _, d := range list {
		fmt.Fprintf(w, "%s  %s\n", d.DeletedAt.Local().Format(timeLayout), d.Title)
	}
	return nil
}

// cmdUndelete brings back the most recently deleted note with a title,
// ignoring case.
func cmdUndelete(ctx context.Context, c *client.Client, w io.Writer, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: scribble undelete <title>")
	}
	list, err := c.Deleted(ctx)
	if err != nil {
		return err
	}
	for _, d := range list {
		if !strings.EqualFold(d.Title, args[0]) {
			continue
		}
		n, err := c.Undelete(ctx, d.ID)
		if err != nil {
			return explain(err, d.Title)
		}
		fmt.Fprintf(w, "undeleted %q\n", n.Title)
		return nil
	}
	return fmt.Errorf("no deleted note titled %q", args[0])
}
