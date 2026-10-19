# scribble (CLI)

`scribble` is the command-line client for a scribble server. It creates,
reads, edits and links notes over the server's HTTP API.

## Build

From the repo root:

```sh
make build-cli        # writes ./scribble, with the version from git
# or
go build -o scribble ./cmd/scribble
```

## Configuration

The CLI has no flags. It reads two environment variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `SCRIBBLE_URL` | `http://localhost:8080` | address of the scribble server |
| `SCRIBBLE_TOKEN` | none (required) | the server's `AUTH_TOKEN` |

```sh
export SCRIBBLE_URL=https://notes.example.com
export SCRIBBLE_TOKEN=change-me
```

If `SCRIBBLE_TOKEN` is not set, every command except `help` fails with
`SCRIBBLE_TOKEN is not set`. If the server does not accept the token, the
error is `the server rejected SCRIBBLE_TOKEN`.

`scribble edit` also reads `VISUAL`, then `EDITOR`, to choose the editor
(fallback: `vi`, or `notepad` on Windows).

## Usage

```
scribble <command> [arguments]
```

Titles that contain spaces must be quoted: `scribble show "Work log"`.
Title lookups ignore case, and a leading `#` in a note's first line is not
part of its title.

Each command exits with status 0 on success and 1 on error. Errors go to
stderr as `scribble: <message>`.

### Commands

| Command | What it does |
| --- | --- |
| `help`, `-h`, `--help` | print usage and the version |
| `new <title> [text...]` | create a note |
| `show <title>` | print a note's Markdown |
| `edit <title>` | edit a note in your editor |
| `append <title> [text...]` | add text at the end of a note; creates it if missing |
| `ls` | list all notes |
| `search <text>` | list notes whose title or body contains the text |
| `links <title>` | list the `[[links]]` in a note |
| `backlinks <title>` | list the notes that link to a note |
| `projects [<name>]` | list `#projects`, or the notes in one |
| `rename <title> <new>` | retitle a note and fix `[[links]]` to it |
| `autolink <title>` | turn mentions of other note titles into `[[links]]` |
| `import <path...>` | import Tomboy `.note` XML files or directories |
| `rm <title>` | delete a note |
| `completion [bash]` | output shell completion script |

Lists (`ls`, `search`, `backlinks`, `projects <name>`) print one title per
line, so they work with `grep`, `head`, `xargs` and so on. `ls` and `search`
show the most recently changed notes first.

### help

```sh
scribble help
scribble            # no arguments does the same
```

### new

Creates a note titled `<title>`. The body is `# <title>`, then a blank line
and the text. The text comes from the remaining arguments, or from stdin when
it is piped and there are no more arguments.

```sh
scribble new Ideas
scribble new "Shopping list" "milk, eggs, bread"
date | scribble new "Build $(git rev-parse --short HEAD)"
```

If a note with that title already exists, the command fails with
`a note titled "..." already exists`.

### show

Prints the note's Markdown to stdout.

```sh
scribble show Ideas
scribble show "Work log" | less
```

### edit

Writes the note to a temporary `.md` file, opens it in your editor, and saves
it when the editor exits. If you made no changes, it prints `no changes`.

```sh
scribble edit Ideas
EDITOR="code --wait" scribble edit Ideas
```

If someone else changed the note while you were editing, the save is refused
and your edit stays in the temporary file. The error message gives its path.

### append

Adds text at the end of a note, after a blank line. If the note does not
exist, it is created. The text comes from the arguments, or from piped stdin.
If another client saves the note at the same time, `append` reads it again and
retries (up to three times).

```sh
scribble append "Work log" "Fixed the login bug"
git log --oneline -5 | scribble append "Work log"
echo "- call the dentist" | scribble append Todo
```

### ls

Lists every note title.

```sh
scribble ls
scribble ls | wc -l
```

### search

Lists the notes whose title or body contains the text. Multiple arguments
are joined with spaces.

```sh
scribble search nginx
scribble search release notes
```

### links

Prints the `[[link]]` targets in a note, one per line.

```sh
scribble links Ideas
```

### backlinks

Lists the notes that contain a `[[link]]` to this note.

```sh
scribble backlinks Ideas
```

### projects

With no argument, lists every `#project` name in use (lowercase, sorted).
With a name, lists the notes tagged with that project.

```sh
scribble projects
scribble projects scribble
```

### rename

Gives a note a new title, and rewrites `[[Old title]]` links in other notes
to point to the new one. The two arguments are the current and the new title.

```sh
scribble rename Ideas "Project ideas"
```

It fails if a note with the new title already exists.

### autolink

Finds plain mentions of other notes' titles in one note, turns them into
`[[links]]`, and saves the note. It prints how many mentions it linked, or
`no mentions to link`.

```sh
scribble autolink "Work log"
```

### import

Imports Tomboy `.note` XML files. A directory argument is searched
recursively for `.note` files. A note whose title already exists is skipped,
not overwritten. At the end, it prints how many notes it created and skipped.

```sh
scribble import ~/.local/share/tomboy
scribble import old/Groceries.note old/Ideas.note
```

### rm

Deletes a note.

```sh
scribble rm "Shopping list"
```

### completion

Outputs the shell completion script for bash. Does not require `SCRIBBLE_TOKEN`.

```sh
# Load in your current shell:
source <(scribble completion)

# Install permanently for the current user:
mkdir -p ~/.local/share/bash-completion/completions
scribble completion > ~/.local/share/bash-completion/completions/scribble

# Or system-wide (requires root):
sudo scribble completion > /etc/bash_completion.d/scribble
```

## Saving shell commands to a note

`append` creates the note the first time and adds to its end after that, so
it is a simple way to keep commands you ran for later.

### With a key binding (Ctrl+R, then Ctrl+X S)

Add this to `~/.bashrc`:

```bash
# Save the current input line to the "Commands" note without running it.
bind -x '"\C-xs": printf "%s\n" "$READLINE_LINE" | scribble append Commands'
```

Press Ctrl+R to find a command in your history, then press Ctrl+X S. Bash
gives the line to the binding in `$READLINE_LINE`, so nothing is quoted or
expanded. The line stays on your prompt: run it, edit it, or clear it with
Ctrl+C. The first time, when the note does not exist yet, `scribble` prints
`created "Commands"` above your prompt.

### With the editor (Ctrl+X Ctrl+E)

Ctrl+X Ctrl+E opens the current line in `$EDITOR`. When you save and quit,
bash runs the edited text. Wrap the command in a quoted heredoc, so that bash
saves it exactly as written:

```bash
scribble append Commands <<'EOF'
awk '{print $1}' access.log | sort | uniq -c | sort -rn | head
EOF
```

Do not wrap it in `echo "..."`. In double quotes, bash expands `$(...)`,
backticks and `$VARS`, so parts of the command can run, and the note gets
the expanded text. `echo '...'` breaks on commands that contain a `'`.

To show the command as a code block in the GUI, put a `` ```sh `` fence
inside the heredoc.
