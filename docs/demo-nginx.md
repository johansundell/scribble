# nginx for the demo, beside the real server

This guide adds the demo site `demo1.sudde.net` to the nginx that already serves `notes.sudde.net`. It is step 4 of [`demo.md`](demo.md) in full. The real site is not changed, and it stays up the whole time.

The result:

```
                       ┌─ server_name notes.sudde.net ─→ upstream scribble      ─→ 127.0.0.1:8082 (scribble-server)
browser ─→ nginx :443 ─┤
                       └─ server_name demo1.sudde.net ─→ upstream scribble_demo ─→ 127.0.0.1:8083 (scribble-demo)
```

nginx picks the site by the name in the request (`server_name`). Each site has its own certificate, its own logs and its own upstream.

Before you start:

- `scribble-demo` runs and answers on `127.0.0.1:8083` ([`demo.md`](demo.md), steps 1–3).
- The DNS `A` record for `demo1.sudde.net` points to the VPS. `dig +short demo1.sudde.net` prints the VPS address.
- The real site works as set up in [`install-vps.md`](install-vps.md), step 4, with `/var/www/certbot` for certificate checks.

Do the steps in order. Each one ends with a check.

## 1. Look at the current setup

```bash
ls -l /etc/nginx/sites-enabled/
sudo nginx -T 2>/dev/null | grep -nE 'upstream|server_name|listen|default_server'
```

**Check:**

- The real site is there (for example `scribble`), with `upstream scribble` and `server_name notes.sudde.net`.
- No other site already uses `demo1.sudde.net` or an upstream called `scribble_demo`. nginx refuses to start if two upstreams have the same name, even in different files. That is why the demo's upstream is not called `scribble`.
- Note any `default_server`. Step 5 explains why it matters.

## 2. Serve the certificate check over HTTP

The demo's full site needs its certificate, and certbot needs the site to get one. So first install only the port 80 part:

```bash
sudo tee /etc/nginx/sites-available/scribble-demo >/dev/null <<'EOF'
server {
    listen 80;
    listen [::]:80;
    server_name demo1.sudde.net;

    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }

    location / {
        return 301 https://$host$request_uri;
    }
}
EOF
sudo ln -s /etc/nginx/sites-available/scribble-demo /etc/nginx/sites-enabled/scribble-demo
sudo nginx -t && sudo systemctl reload nginx
```

`nginx -t` checks every site before the reload. If it fails, nothing changes, and the real site keeps running.

**Check:** from your own machine, `curl -sI http://demo1.sudde.net/` shows `301` and `Location: https://demo1.sudde.net/`.

## 3. Get the certificate

```bash
sudo certbot certonly --webroot -w /var/www/certbot -d demo1.sudde.net \
  --deploy-hook 'systemctl reload nginx'
```

The certificate is separate from the real site's. `--deploy-hook` reloads nginx after each renewal, so nginx uses the new certificate. certbot renews it by itself (`systemctl list-timers certbot` shows when).

**Check:** `sudo ls /etc/letsencrypt/live/demo1.sudde.net/` lists `fullchain.pem` and `privkey.pem`, and `sudo certbot renew --dry-run --cert-name demo1.sudde.net` ends with `Congratulations`.

## 4. Install the full site

Copy [`docs/nginx/demo1.sudde.net.conf`](nginx/demo1.sudde.net.conf) over the file from step 2. It adds the HTTPS part, which forwards to `127.0.0.1:8083`.

```bash
# on your machine
scp docs/nginx/demo1.sudde.net.conf you@your.vps:/tmp/

# on the VPS
sudo install -o root -g root -m 0644 /tmp/demo1.sudde.net.conf /etc/nginx/sites-available/scribble-demo
rm /tmp/demo1.sudde.net.conf
sudo nginx -t && sudo systemctl reload nginx
```

The file differs from the real site's [`notes.sudde.net.conf`](nginx/notes.sudde.net.conf) only in these places:

| | Real site | Demo |
|---|---|---|
| `upstream` | `scribble` → `127.0.0.1:8082` | `scribble_demo` → `127.0.0.1:8083` |
| `server_name` | `notes.sudde.net` | `demo1.sudde.net` |
| Certificate | `/etc/letsencrypt/live/notes.sudde.net/` | `/etc/letsencrypt/live/demo1.sudde.net/` |
| Logs | `/var/log/nginx/notes.sudde.net.*.log` | `/var/log/nginx/demo1.sudde.net.*.log` |
| `proxy_pass` | `http://scribble` | `http://scribble_demo` |

The rest is the same: the `Host` and `X-Forwarded-For` headers that the web app needs, the 5 MB body limit and the timeouts.

**Check:** from your own machine:

```bash
curl -s -H 'Accept: application/json' https://demo1.sudde.net/healthz
curl -s -o /dev/null -w '%{http_code}\n' -H 'Authorization: Bearer demo' https://demo1.sudde.net/notes
curl -s -o /dev/null -w '%{http_code}\n' -H 'Authorization: Bearer demo' https://notes.sudde.net/notes
```

- The first prints JSON with `"dbStatus":"OK"`.
- The second prints `200`: the demo takes the token `demo`.
- The third prints `401`: the real server does not. If it prints `200`, both names reach the same server; see the troubleshooting section.

Both servers report `"name":"scribble-server"` in `/healthz`, so use the token to tell them apart, not the name.

Then open `https://demo1.sudde.net/` in a browser. The login screen shows the demo hint, and a code from an authenticator set up with the token `demo` logs in. `https://notes.sudde.net/` still shows the real server's login screen, without the hint.

## 5. Keep unknown names away from the real site

When a request has a name that no site has, nginx sends it to the port's `default_server`, or else to the first site it loaded. That is usually the real site. So a visitor who uses the VPS address, or a name that is not set up, gets the real server's login screen.

This is not a leak, because the login codes and the token still protect the notes. But it is better to close it. If step 1 showed no `default_server` for port 443, add a site that drops such requests:

```bash
sudo tee /etc/nginx/sites-available/000-default >/dev/null <<'EOF'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    listen 443 ssl default_server;
    listen [::]:443 ssl default_server;
    ssl_reject_handshake on;
    return 444;
}
EOF
sudo ln -s /etc/nginx/sites-available/000-default /etc/nginx/sites-enabled/000-default
sudo nginx -t && sudo systemctl reload nginx
```

`ssl_reject_handshake` (nginx 1.19.4 or newer) ends the TLS handshake without a certificate. `return 444` closes plain HTTP connections without an answer. If another project on the VPS relies on being the default site, skip this step.

**Check:** `curl -skI https://<the VPS address>/` fails, and the checks in step 4 still pass.

## Troubleshooting

- **`nginx: [emerg] duplicate upstream "scribble"`.** The demo's site uses the real site's upstream name. Use the file from the repo, which calls it `scribble_demo`.
- **`cannot load certificate "/etc/letsencrypt/live/demo1.sudde.net/fullchain.pem"`.** The full site was installed before the certificate existed. Go back to step 2.
- **certbot: `Invalid response from http://demo1.sudde.net/.well-known/acme-challenge/…`.** DNS doesn't point to the VPS yet, or the port 80 part of step 2 is not enabled. Check `dig +short demo1.sudde.net` and `ls -l /etc/nginx/sites-enabled/`.
- **`502 Bad Gateway` on the demo.** nginx works, but nothing answers on `127.0.0.1:8083`. Check `systemctl status scribble-demo` and `PORT` in `/opt/scribble-demo/.env`. Between 04:00 and a few seconds after, the nightly reset restarts the demo, so a short 502 then is normal.
- **The demo shows the real server's notes, or the real site takes the token `demo`.** Both names reach the same upstream. Check that the demo's file has `proxy_pass http://scribble_demo;`, and that `upstream scribble_demo` has port `8083`.
- **The browser warns about the certificate on the demo.** nginx sent another site's certificate, usually because the demo's site is not enabled. Run `sudo nginx -T | grep -A3 'server_name demo1'`.
- **The web login answers `403` (`cross-origin request refused`).** The server compares the form's `Origin` with the `Host` it gets. Check that the site has `proxy_set_header Host $host;`.
- **Errors for the demo only.** Look in `/var/log/nginx/demo1.sudde.net.error.log`. The real site logs to its own files.
