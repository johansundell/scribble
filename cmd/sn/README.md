# sn (CLI)

`sn` is the command-line client for a scribble server. It creates,
reads, edits and links notes over the server's HTTP API.

## Install

On Linux and macOS (amd64 or arm64), install or upgrade to the latest
release without Go or a checkout:

```sh
curl -fsSL https://raw.githubusercontent.com/johansundell/scribble/master/scripts/install-sn.sh | sh
```

It downloads the release archive for your system, checks its SHA-512
checksum and installs `sn` to `/usr/local/bin`, with `sudo` if you can't
write there. Settings go on the `sh` side of the pipe: a version as
`... | sh -s v1.0.1` (or `... | VERSION=v1.0.1 sh`), another directory as
`... | INSTALL_DIR=~/.local/bin sh`. On Windows, download
`scribble_<version>_windows_amd64.tar.gz` from
https://github.com/johansundell/scribble/releases/latest.

## Build

From the repo root:

```sh
make build-cli        # writes ./sn, with the version from git
# or
go build -o sn ./cmd/sn
```

## Configuration

Apart from `show -r` and `restore -y`, the CLI has no flags. It reads two
environment variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `SCRIBBLE_URL` | `https://notes.sudde.net` | address of the scribble server |
| `SCRIBBLE_TOKEN` | none (required) | the server's `AUTH_TOKEN` |

```sh
export SCRIBBLE_URL=https://notes.example.com
export SCRIBBLE_TOKEN=change-me
```

If `SCRIBBLE_TOKEN` is not set, every command except `help`, `completion` and `version` fails with
`SCRIBBLE_TOKEN is not set`. If the server does not accept the token, the
error is `the server rejected SCRIBBLE_TOKEN`.

`sn edit` also reads `VISUAL`, then `EDITOR`, to choose the editor
(fallback: `vi`, or `notepad` on Windows). `sn diff` reads `NO_COLOR`
(see [diff](#diff)).

## Usage

```
sn <command> [arguments]
```

Titles that contain spaces must be quoted: `sn show "Work log"`.
Title lookups ignore case, and a leading `#` in a note's first line is not
part of its title.

Each command exits with status 0 on success and 1 on error. Errors go to
stderr as `sn: <message>`.

### Commands

| Command | What it does |
| --- | --- |
| `help`, `-h`, `--help` | print usage and the version |
| `new <title> [text...]` | create a note |
| `show <title> [-r <rev>]` | print a note's Markdown, or that of an old version |
| `edit <title>` | edit a note in your editor |
| `append <title> [text...]` | add text at the end of a note; creates it if missing |
| `ls` | list all notes, pinned ones first |
| `search <text>` | list notes whose title or body contains the text |
| `links <title>` | list the `[[links]]` in a note |
| `backlinks <title>` | list the notes that link to a note |
| `projects [<name>]` | list `#projects`, or the notes in one |
| `pin <title>`, `unpin <title>` | keep a note at the top of lists, or stop |
| `rename <title> <new>` | retitle a note and fix `[[links]]` to it |
| `autolink <title>` | turn mentions of other note titles into `[[links]]` |
| `import <path...>` | import Tomboy `.note` XML files or directories |
| `rm <title>` | delete a note |
| `history <title>` | list a note's versions, newest first |
| `diff <title> [<rev> [<rev2>]]` | show what changed between two versions of a note |
| `restore [-y] <title> <rev>` | make an old version of a note its current text |
| `deleted` | list deleted notes, most recently deleted first |
| `undelete <title>` | bring back a deleted note |
| `completion [bash]` | output shell completion script |
| `version` | print the client's and the server's versions and API levels, and the server's schema version |

Lists (`ls`, `search`, `backlinks`, `projects <name>`) print one title per
line, so they work with `grep`, `head`, `xargs` and so on. `ls` and `search`
show pinned notes first, then the most recently changed. In `ls` only, a
pinned note's title starts with `📌 `; strip it (`sed 's/^📌 //'`) before
passing titles on.

### help

```sh
sn help
sn            # no arguments does the same
```

### new

Creates a note titled `<title>`. The body is `# <title>`, then a blank line
and the text. The text comes from the remaining arguments, or from stdin when
it is piped and there are no more arguments.

```sh
sn new Ideas
sn new "Shopping list" "milk, eggs, bread"
date | sn new "Build $(git rev-parse --short HEAD)"
```

If a note with that title already exists, the command fails with
`a note titled "..." already exists`.

### show

Prints the note's Markdown to stdout. With `-r <rev>`, prints the Markdown
of that version of the note instead (see [history](#history)). A revision
may be written `12` or `r12`, and `-r` may come before or after the title.

```sh
sn show Ideas
sn show "Work log" | less
sn show Ideas -r 3
sn show -r r3 Ideas > ideas-r3.md
```

If the note has no such revision, the error is
`no revision r3 of "Ideas" (see: sn history Ideas)`.

Because `show` and `restore` take flags, a title that starts with `-` goes
after `--`: `sn show -- -todo`.

### edit

Writes the note to a temporary `.md` file, opens it in your editor, and saves
it when the editor exits. If you made no changes, it prints `no changes`.

```sh
sn edit Ideas
EDITOR="code --wait" sn edit Ideas
```

If someone else changed the note while you were editing, the save is refused
and your edit stays in the temporary file. The error message gives its path.

### append

Adds text at the end of a note, after a blank line. If the note does not
exist, it is created. The text comes from the arguments, or from piped stdin.
If another client saves the note at the same time, `append` reads it again and
retries (up to three times).

```sh
sn append "Work log" "Fixed the login bug"
git log --oneline -5 | sn append "Work log"
echo "- call the dentist" | sn append Todo
```

### ls

Lists every note title, pinned notes first, marked `📌 `.

```sh
sn ls
sn ls | wc -l
```

### search

Lists the notes whose title or body contains the text. Multiple arguments
are joined with spaces.

```sh
sn search nginx
sn search release notes
```

### links

Prints the `[[link]]` targets in a note, one per line.

```sh
sn links Ideas
```

### backlinks

Lists the notes that contain a `[[link]]` to this note.

```sh
sn backlinks Ideas
```

### projects

With no argument, lists every `#project` name in use (lowercase, sorted).
With a name, lists the notes tagged with that project.

```sh
sn projects
sn projects scribble
```

### pin, unpin

`pin` keeps a note at the top of every note list: in `sn ls`, the web app and
the desktop app. `unpin` lets it sort by date again. Pinning isn't an edit: the
note's revision and history stay as they were.

```sh
sn pin "Shopping list"
sn unpin "Shopping list"
```

### rename

Gives a note a new title, and rewrites `[[Old title]]` links in other notes
to point to the new one. The two arguments are the current and the new title.

```sh
sn rename Ideas "Project ideas"
```

It fails if a note with the new title already exists.

### autolink

Finds plain mentions of other notes' titles in one note, turns them into
`[[links]]`, and saves the note. It prints how many mentions it linked, or
`no mentions to link`.

```sh
sn autolink "Work log"
```

### import

Imports Tomboy `.note` XML files. A directory argument is searched
recursively for `.note` files. A note whose title already exists is skipped,
not overwritten. At the end, it prints how many notes it created and skipped.

```sh
sn import ~/.local/share/tomboy
sn import old/Groceries.note old/Ideas.note
```

### rm

Deletes a note.

```sh
sn rm "Shopping list"
```

### history

Lists the versions the server keeps of a note, newest first, one per line:
the revision, the local time it was saved, why it was saved, and the title
it had then. The current version is marked `(current)`.

```sh
$ sn history Ideas
r7  2026-10-20 14:02  edit  Ideas  (current)
r5  2026-10-20 13:40  rename  Ideas
r4  2026-10-19 09:12  edit  Draft
r1  2026-10-18 21:30  create  Draft
```

The reason is `create`, `edit`, `rename`, `link-rewrite` (another note was
renamed and a `[[link]]` in this one was fixed), `restore`, `delete` or
`undelete`. Saves made within a few minutes of each other are kept as one
version, so revision numbers can have gaps.

### diff

Prints a unified diff between two versions of a note, with three lines of
context:

- `sn diff <title>` compares the version before the current one with
  the current text.
- `sn diff <title> <rev>` compares that version with the current text.
- `sn diff <title> <rev> <rev2>` compares the first version with the
  second.

```sh
$ sn diff Ideas 4
--- Draft r4
+++ Ideas r7
@@ -1,3 +1,4 @@
-# Draft
+# Ideas
 one
 two
+three
```

The headers name each version's title and revision. The diff is coloured
only when stdout is a terminal, and never when `NO_COLOR` is set. It prints
nothing when the versions are the same, and exits with 0 either way.

### restore

Makes an old version of a note its current text, as a new revision, and
prints `restored "<title>" to r<rev>`. The old version's title comes back
with it. When that changes the title, the message ends with
`(now titled "<new title>")`.
Nothing is lost: the text it replaces stays in the history.

When stdin is a terminal, it asks `Restore "Ideas" to r4? [y/N]` first, or
`Restore "Ideas" to r4 (renames it to "Draft")? [y/N]` when the old version
had another title.
`-y` skips the question. When stdin is piped, it does not ask, so scripts
work.

```sh
sn restore Ideas 4
sn restore -y Ideas r4
```

It fails with:

- `a note titled "..." already exists` when another note now has the old
  version's title;
- `the note was changed elsewhere since you started` when someone saved the
  note after `restore` read it, for example while the question was shown.
  Run it again to restore anyway.

### deleted

Lists deleted notes, most recently deleted first, with the local time each
was deleted.

```sh
$ sn deleted
2026-10-20 13:55  Shopping list
2026-10-12 08:03  Old ideas
```

### undelete

Brings back the most recently deleted note with that title (ignoring case),
with the text it had when it was deleted.

```sh
sn undelete "Shopping list"
```

It fails with `no deleted note titled "..."` when there is none, and with
`a note titled "..." already exists` when a live note has taken the title.
Rename or delete that note first.

### completion

Outputs the shell completion script for bash. Does not require `SCRIBBLE_TOKEN`.

It completes commands, note titles (also after `show -r <rev>` and
`restore -y`), deleted notes' titles for `undelete`, and projects.

```sh
# Load in your current shell:
source <(sn completion)

# Install permanently for the current user:
mkdir -p ~/.local/share/bash-completion/completions
sn completion > ~/.local/share/bash-completion/completions/sn

# Or system-wide (requires root):
sudo sn completion > /etc/bash_completion.d/sn
```

### version

Prints the CLI's version and API level, then the server's, with the
server's database schema version when it has one (SQLite only), and says
`update available` when the server runs a newer release, with the
command that installs it on Linux and macOS. `dev` builds
never say so. It asks `/healthz`, so it does not need `SCRIBBLE_TOKEN`.
It is the only command that checks for a newer release, so scripts using
the others see no extra output.

```sh
$ sn version
client v0.0.11 (API level 1)
server v0.0.12 (API level 2, schema 3)
update available: https://github.com/johansundell/scribble/releases/latest
run: curl -fsSL https://raw.githubusercontent.com/johansundell/scribble/master/scripts/install-sn.sh | sh
```

If the server answers 426 because this CLI is too old for it, every
command fails with (the `or run:` part on Linux and macOS only):

```
sn: this version (v0.0.11) is too old for the server; download the latest from https://github.com/johansundell/scribble/releases/latest or run: curl -fsSL https://raw.githubusercontent.com/johansundell/scribble/master/scripts/install-sn.sh | sh
```

## Saving shell commands to a note

`append` creates the note the first time and adds to its end after that, so
it is a simple way to keep commands you ran for later.

### With a key binding (Ctrl+R, then Ctrl+X S)

Add this to `~/.bashrc`:

```bash
# Save the current input line to the "Commands" note without running it.
bind -x '"\C-xs": printf "%s\n" "$READLINE_LINE" | sn append Commands'
```

Press Ctrl+R to find a command in your history, then press Ctrl+X S. Bash
gives the line to the binding in `$READLINE_LINE`, so nothing is quoted or
expanded. The line stays on your prompt: run it, edit it, or clear it with
Ctrl+C. The first time, when the note does not exist yet, `sn` prints
`created "Commands"` above your prompt.

### With the editor (Ctrl+X Ctrl+E)

Ctrl+X Ctrl+E opens the current line in `$EDITOR`. When you save and quit,
bash runs the edited text. Wrap the command in a quoted heredoc, so that bash
saves it exactly as written:

```bash
sn append Commands <<'EOF'
awk '{print $1}' access.log | sort | uniq -c | sort -rn | head
EOF
```

Do not wrap it in `echo "..."`. In double quotes, bash expands `$(...)`,
backticks and `$VARS`, so parts of the command can run, and the note gets
the expanded text. `echo '...'` breaks on commands that contain a `'`.

To show the command as a code block in the GUI, put a `` ```sh `` fence
inside the heredoc.
