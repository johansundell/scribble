# Shell snippets

Commands I keep looking up. #scribble

Find the biggest folders:

```bash
du -sh -- */ | sort -h | tail -n 10
```

Follow a service's log:

```bash
journalctl -u scribble-server -f
```

Check that the server answers:

```bash
curl -s -H 'Accept: application/json' https://demo1.sudde.net/healthz
```

The last one is also in [[Using sn with the demo]].
