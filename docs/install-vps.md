# Installing the server on a VPS for the first time

This guide installs `scribble-server` on a fresh Linux VPS, behind nginx with HTTPS. Then you can turn on automatic deploys with [`deploy-vps.md`](deploy-vps.md), which starts from what this guide sets up.

The result:

- `scribble-server` in `/opt/scribble`, running as the systemd service `scribble-server`, with its settings in `/opt/scribble/.env` and its notes in `/opt/scribble/scribble-server.db`.
- The server listening on `127.0.0.1:8082` only, so plain HTTP never leaves the machine.
- nginx in front, serving HTTPS at your domain (`notes.example.com` below).
- The web app at `https://notes.example.com/`, if you want it.

You need a VPS running Debian or Ubuntu, a domain name with a DNS `A` record that points to it, and `sudo`.

Do the steps in order. Each one ends with a check.

## 1. Install the binary

Pick the latest release on the [releases page](https://github.com/johansundell/scribble/releases), for example `v0.0.9`. On the VPS:

```bash
TAG=v0.0.9
ARCH=amd64   # arm64 on an ARM VPS (uname -m prints aarch64)
NAME=scribble_${TAG}_linux_${ARCH}

cd /tmp
curl -fsSLO https://github.com/johansundell/scribble/releases/download/$TAG/$NAME.tar.gz
curl -fsSLO https://github.com/johansundell/scribble/releases/download/$TAG/$NAME.sha512
sha512sum -c $NAME.sha512
tar -xzf $NAME.tar.gz scribble-server

sudo install -d -o root -g root -m 0755 /opt/scribble
sudo install -o root -g root -m 0755 scribble-server /opt/scribble/scribble-server
rm scribble-server $NAME.tar.gz $NAME.sha512
```

**Check:** `sha512sum -c` printed `OK`, and `ls -l /opt/scribble` shows `scribble-server`.

## 2. Write the settings

The server reads `.env` from the folder its binary is in. Create the secrets first:

```bash
openssl rand -base64 33   # AUTH_TOKEN: the CLI and the desktop app use this
openssl rand -base64 33   # SESSION_SECRET: only if you want the web app
```

Then create `/opt/scribble/.env` and paste the values in:

```bash
sudo install -o root -g root -m 0600 /dev/null /opt/scribble/.env
sudo nano /opt/scribble/.env
```

```sh
PORT=127.0.0.1:8082
AUTH_TOKEN=<the first value>

# nginx runs on this host: believe its X-Forwarded-For, and nobody else's.
TRUSTED_PROXIES=127.0.0.1

# The web app at /. Leave WEB_PIN out to keep it off.
WEB_PIN=<6-12 digits>
SESSION_SECRET=<the second value>
```

- `.env` holds the token and the PIN, so only root may read it (`0600`).
- Without `AUTH_TOKEN`, the server makes up a new token at every start and writes it to the log. Always set it.
- `TRUSTED_PROXIES=127.0.0.1` is needed behind nginx: without it, every visitor gets nginx's address, and the web app's limit of 5 wrong PINs per address applies to everyone at once.
- `WEB_PIN` must be 6–12 digits. `SESSION_SECRET` must be at least 32 characters and not the same as `AUTH_TOKEN`. If either is wrong, the server won't start, and says why in the log.

All settings are listed in [`service-template.md`](service-template.md#configuration). The web app is described in the [README](../README.md#web-app).

**Check:** `sudo cat /opt/scribble/.env` shows your values, and `ls -l /opt/scribble/.env` shows `-rw-------`.

## 3. Install and start the service

```bash
cd /opt/scribble
sudo ./scribble-server -service install
sudo ./scribble-server -service start
```

This creates the systemd service `scribble-server`, which starts on boot.

**Check:**

```bash
systemctl status scribble-server --no-pager
curl -s -H 'Accept: application/json' http://127.0.0.1:8082/healthz
```

The service is `active (running)`, and `curl` prints JSON with `"dbStatus":"OK"`. If the web app is on, `journalctl -u scribble-server -n 20` shows `web app is on at /`. If the service doesn't start, that log tells you why, for example a wrong `WEB_PIN`.

## 4. Put nginx in front

Install nginx and certbot, and get a certificate:

```bash
sudo apt install nginx certbot
sudo install -d /var/www/certbot
```

Copy [`docs/nginx/notes.sudde.net.conf`](nginx/notes.sudde.net.conf) to `/etc/nginx/sites-available/scribble`, and replace `notes.sudde.net` with your domain everywhere in it. The file expects the certificate to exist already, so comment out the whole `listen 443` server block for now. Then:

```bash
sudo ln -s /etc/nginx/sites-available/scribble /etc/nginx/sites-enabled/scribble
sudo nginx -t && sudo systemctl reload nginx
sudo certbot certonly --webroot -w /var/www/certbot -d notes.example.com
```

Now uncomment the `listen 443` block again and reload:

```bash
sudo nginx -t && sudo systemctl reload nginx
```

The config passes `Host` and `X-Forwarded-For` to scribble. The web app needs both: `Host` for its check that a form came from your own site, and `X-Forwarded-For` (with `TRUSTED_PROXIES`) for the visitor's address.

**Check:** from your own machine:

```bash
curl -s -H 'Accept: application/json' https://notes.example.com/healthz
curl -s -H "Authorization: Bearer <AUTH_TOKEN>" https://notes.example.com/notes
```

The first prints the same JSON as in step 3. The second prints `[]` (no notes yet).

## 5. Try the clients

- **CLI and desktop app:** set `SCRIBBLE_URL=https://notes.example.com` and `SCRIBBLE_TOKEN=<AUTH_TOKEN>` (see the [README](../README.md#quick-start)).
- **Web app:** open `https://notes.example.com/` in a browser or on your phone, and enter the PIN.

**Check:** `sn new "Hello" "first note"` works, and the web app shows the page with "Log out" after the PIN.

## Next

- Turn on automatic deploys of new releases: [`deploy-vps.md`](deploy-vps.md).
- Schedule daily backups of the notes: [`deploy-vps.md`, step 9](deploy-vps.md#9-schedule-daily-backups), and copy them off the server (see [Backups](../README.md#backups) in the README).
- To change a setting, edit `/opt/scribble/.env` and run `sudo systemctl restart scribble-server`. Changing `WEB_PIN` or `SESSION_SECRET` this way also clears a web login lockout. Changing `SESSION_SECRET` logs every browser out.
