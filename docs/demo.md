# The public demo at demo1.sudde.net

This guide sets up the public demo (#161) as a second instance on the VPS that already runs `notes.sudde.net` ([`install-vps.md`](install-vps.md), [`deploy-vps.md`](deploy-vps.md)). Anyone can open the web app with an authenticator app set up from the QR code on its login screen, or point `sn` or `scribble-gui` at it with a published token. Every night a timer replaces its database and loads the sample notes from [`demo/notes`](../demo/notes) again.

The result:

- `scribble-server` and `sn` in `/opt/scribble-demo`, running as the systemd service `scribble-demo`, with its own `.env` and its notes in `/opt/scribble-demo/scribble-server.db`.
- The demo listening on `127.0.0.1:8083`. The real server stays on `127.0.0.1:8082`.
- nginx serving HTTPS at `demo1.sudde.net`.
- A timer that runs [`scripts/demo-reset.sh`](../scripts/demo-reset.sh) at 04:00 Europe/Stockholm.
- `deploy.sh` updating both instances on every release.

The public token, the TOTP secret and the session secret are only in the demo's `.env`. Nothing about the demo is built into the binaries, so a real server that misses a setting never runs with them.

You need a DNS `A` record for `demo1.sudde.net` that points to the VPS, and `sudo`. Do the steps in order. Each one ends with a check.

## 1. Install the binaries

Copy the real server's binary, and get `sn` from the same release. The reset script loads the sample notes with it. On the VPS:

```bash
TAG=$(curl -s -H 'Accept: application/json' http://127.0.0.1:8082/healthz | grep -o '"version":"[^"]*"' | cut -d'"' -f4)
ARCH=amd64   # arm64 on an ARM VPS (uname -m prints aarch64)
NAME=scribble_${TAG}_linux_${ARCH}

cd /tmp
curl -fsSLO https://github.com/johansundell/scribble/releases/download/$TAG/$NAME.tar.gz
curl -fsSLO https://github.com/johansundell/scribble/releases/download/$TAG/$NAME.sha512
sha512sum -c $NAME.sha512
tar -xzf $NAME.tar.gz scribble-server sn

sudo install -d -o root -g root -m 0755 /opt/scribble-demo
sudo install -o root -g root -m 0755 scribble-server sn /opt/scribble-demo/
rm scribble-server sn $NAME.tar.gz $NAME.sha512
```

**Check:** `sha512sum -c` printed `OK`, and `ls -l /opt/scribble-demo` shows `scribble-server` and `sn`.

> Use a copy, not a link to `/opt/scribble/scribble-server`. The server reads the `.env` beside its binary, and a link would make the demo read the real server's `.env`.

## 2. Write the settings

Make a new session secret. Don't reuse the real server's, and don't use one that was ever published:

```bash
openssl rand -base64 33
```

Create `/opt/scribble-demo/.env`:

```bash
sudo install -o root -g root -m 0600 /dev/null /opt/scribble-demo/.env
sudo nano /opt/scribble-demo/.env
```

```sh
PORT=127.0.0.1:8083
AUTH_TOKEN=demo
WEB_TOTP_SECRET=<the output of: head -c 20 /dev/urandom | base32>
SESSION_SECRET=<the value from openssl>
WEB_LOGIN_HINT="Demo: scan the QR code with an authenticator app. Notes are reset every night."
PUBLIC_DEMO=true
TRUSTED_PROXIES=127.0.0.1
```

- `AUTH_TOKEN=demo` is the token that the README tells people to put in their clients. It also opens the web app's **Set up authenticator** page, so the TOTP secret is public in effect, like the token.
- `PUBLIC_DEMO=true` (#175) shows the TOTP secret on the login screen: a QR code, a link that opens the authenticator app on a phone, and the secret as text. Visitors need no token for the web app. Never set it on the real server.
- A code logs in only once (#172), and every visitor's app makes the same codes, so two visitors who log in within the same 30 seconds may need to wait for the next code.
- The session secret stays on the VPS. It is not in the repo, because anyone who knows it can make a login cookie.
- `TRUSTED_PROXIES=127.0.0.1` gives each visitor their own limit of 5 wrong codes in 15 minutes. With `PUBLIC_DEMO=true`, wrong codes never lock web login for everyone, so one visitor can't lock out the rest.

**Check:** `ls -l /opt/scribble-demo/.env` shows `-rw-------`.

## 3. Install and start the service

`scribble-server -service install` always names its service `scribble-server`, which the real server already uses. So write the demo's unit by hand:

```bash
sudo tee /etc/systemd/system/scribble-demo.service >/dev/null <<'EOF'
[Unit]
Description=scribble demo (demo1.sudde.net)
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/opt/scribble-demo/scribble-server
WorkingDirectory=/opt/scribble-demo
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now scribble-demo
```

**Check:**

```bash
systemctl status scribble-demo --no-pager
curl -s -H 'Accept: application/json' http://127.0.0.1:8083/healthz
```

The service is `active (running)`, and `curl` prints JSON with `"dbStatus":"OK"`. `journalctl -u scribble-demo -n 20` shows `PUBLIC_DEMO is on`.

## 4. Put nginx in front

Follow [`demo-nginx.md`](demo-nginx.md). It adds the site `demo1.sudde.net` beside the real one, gets its certificate, and checks that each name reaches its own server. In short: [`docs/nginx/demo1.sudde.net.conf`](nginx/demo1.sudde.net.conf) is the real site's file with the demo's name, certificate, logs and port, and an upstream called `scribble_demo`, so the two sites don't clash.

**Check:** from your own machine:

```bash
curl -s -H 'Accept: application/json' https://demo1.sudde.net/healthz
curl -s -H 'Authorization: Bearer demo' https://demo1.sudde.net/notes
```

The first prints the same JSON as in step 3. The second prints `[]`. `https://notes.sudde.net/healthz` still answers with the real server's JSON.

## 5. Install the reset script and the sample notes

From a checkout of the repo, on your machine:

```bash
scp scripts/demo-reset.sh you@your.vps:/tmp/
scp -r demo/notes you@your.vps:/tmp/demo-notes
```

On the VPS, make them root-owned, because the timer runs the script as root:

```bash
sudo install -o root -g root -m 0755 /tmp/demo-reset.sh /opt/scribble-demo/demo-reset.sh
sudo rm -rf /opt/scribble-demo/notes
sudo install -d -o root -g root -m 0755 /opt/scribble-demo/notes
sudo install -o root -g root -m 0644 /tmp/demo-notes/*.md /opt/scribble-demo/notes/
rm -r /tmp/demo-reset.sh /tmp/demo-notes
```

Run the first reset by hand:

```bash
sudo /opt/scribble-demo/demo-reset.sh
```

The script stops `scribble-demo`, removes its database, starts it, waits for `/healthz`, creates one note for each `notes/*.md` file, pins "Welcome to the demo", and edits "Work log" and "Shell snippets" so that they have a history.

**Check:** it ends with `demo reset: 6 notes loaded` (one per file). Open `https://demo1.sudde.net/`: the login screen shows the hint and the QR code, a code from the app logs in, and "Welcome to the demo" is at the top.

> The VPS runs its own copies. If `scripts/demo-reset.sh` or `demo/notes` changes in the repo, repeat this step.

## 6. Reset every night

```bash
sudo tee /etc/systemd/system/scribble-demo-reset.service >/dev/null <<'EOF'
[Unit]
Description=Reset the scribble demo

[Service]
Type=oneshot
ExecStart=/opt/scribble-demo/demo-reset.sh
EOF
sudo tee /etc/systemd/system/scribble-demo-reset.timer >/dev/null <<'EOF'
[Unit]
Description=Reset the scribble demo every night

[Timer]
OnCalendar=*-*-* 04:00:00 Europe/Stockholm
Persistent=true

[Install]
WantedBy=timers.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now scribble-demo-reset.timer
```

`Persistent=true` runs a reset that was missed while the VPS was off as soon as it is on again.

**Check:** `systemctl list-timers scribble-demo-reset` shows the next run at 04:00 Stockholm time (printed in the server's time zone). The next morning, `journalctl -u scribble-demo-reset -n 20` ends with `demo reset: 6 notes loaded`.

## 7. Deploy both instances

[`scripts/deploy.sh`](../scripts/deploy.sh) updates the demo after the real server when `/opt/scribble-demo/scribble-server` exists. It installs `sn` there too, so the reset uses the same release. If the demo doesn't answer after a deploy, only the demo is rolled back, and the deploy fails so that you see it. The real server keeps the new release.

The VPS runs its own copy of `deploy.sh`, so install the new one as in [`deploy-vps.md`, step 2](deploy-vps.md#2-install-the-deploy-script):

```bash
# on your machine
scp scripts/deploy.sh you@your.vps:/tmp/deploy.sh

# on the VPS
sudo install -o root -g root -m 0755 /tmp/deploy.sh /opt/scribble/deploy.sh
rm /tmp/deploy.sh
```

**Check:** deploy the running version again, for example:

```bash
sudo /opt/scribble/deploy.sh v0.0.9
```

It prints `deployed <tag> to scribble-server`, then `deployed <tag> to scribble-demo`, and ends with `deployed <tag>`.

## Troubleshooting

- **The demo won't start: `address already in use`.** Another service is on port 8083. Pick a free port and change it in `.env`, in the nginx site, in `deploy.sh` (`DEMO_HEALTH_URL`) and in the reset's environment (`HEALTH_URL` and `SCRIBBLE_URL`, see the top of `demo-reset.sh`).
- **`sn: the server rejected SCRIBBLE_TOKEN` in the reset.** `AUTH_TOKEN` in `/opt/scribble-demo/.env` is not `demo`.
- **`no sample notes in /opt/scribble-demo/notes`.** Repeat step 5.
- **`sn: a note titled "…" already exists` in the reset.** Two files in `demo/notes` have the same title (the first line), or the database was not removed. Check that `SQLITE_PATH` is not set in the demo's `.env`: the script removes `scribble-server.db` beside the binary.
- **Visitors are locked out of the web app.** `PUBLIC_DEMO=true` is missing from `.env`: without it, 20 wrong codes in 24 hours lock web login until a restart. Add it, then `sudo systemctl restart scribble-demo`.
- **The login screen has no QR code.** `PUBLIC_DEMO=true` is missing from `.env`, or the binary is older than the release with #175.
