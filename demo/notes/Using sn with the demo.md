# Using sn with the demo

`sn` is the command line client. #scribble

Download it from the [releases page](https://github.com/johansundell/scribble/releases), then point it at the demo in `~/.scribble`:

```ini
url=https://demo1.sudde.net
token=demo
```

Then try:

```bash
sn ls
sn show "Welcome to the demo"
sn new "From the terminal" "Written with sn"
sn history "Work log"
sn diff "Work log"
```

For the desktop app (`scribble-gui`), enter the same URL and token in **File > Settings…**. It keeps a copy of the notes. After the nightly reset it notices that the demo was replaced, loads the fresh notes, and sends edits it had not sent yet as new notes.

Back to [[Welcome to the demo]].
