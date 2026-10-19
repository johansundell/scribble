# Deploying releases to a VPS automatically

This guide sets up a VPS so that each new GitHub release of scribble is installed on it automatically. The setup has three parts:

- **On the server:** [`scripts/deploy.sh`](../scripts/deploy.sh) downloads the release for a tag, checks its SHA-512 checksum, replaces the `scribble-server` binary and restarts the service. If `/healthz` does not answer within about 30 seconds, it puts the previous binary back.
- **On GitHub:** the `Deploy` workflow ([`.github/workflows/deploy.yml`](../.github/workflows/deploy.yml)) runs when a release is published. It connects to the server over SSH and sends the tag. That is all it does.
- **Between them:** a `deploy` user whose SSH key for scribble can run `deploy.sh` and nothing else. GitHub never gets a shell or file access on the server.

The `deploy` user can be shared with other projects on the same server. The forced command is set per SSH key, so each project's key runs only that project's script. If the user already exists, skip creating it in step 3, and in step 4 add the new key without removing the others.

nginx needs no changes, because scribble keeps listening on the same port.

The guide assumes this setup:

- A Linux VPS, with `scribble-server` installed in `/opt/scribble` as the systemd service `scribble-server` (`./scribble-server -service install`).
- nginx in front of scribble, which listens on `127.0.0.1:8082` (as in [`docs/nginx`](nginx/notes.sudde.net.conf)).
- Releases made with `make release`, which uploads `scribble_<tag>_linux_<arch>.tar.gz` and a `.sha512` file to the GitHub release. The archive holds `scribble-server`, the `scribble` CLI and, on amd64, `scribble-gui`. Only `scribble-server` is installed.

Do the steps in order. Each one ends with a check.

## 1. Check the current install

On the VPS:

```bash
systemctl cat scribble-server
```

Look at `ExecStart`. It must be `/opt/scribble/scribble-server` (sometimes with arguments). Then check the port in the service's `.env`:

```bash
grep '^PORT' /opt/scribble/.env
curl -s -H 'Accept: application/json' http://127.0.0.1:8082/healthz
```

The `curl` command should print JSON with `"dbStatus":"OK"` and the running `version`. A server older than the release that added `/healthz` answers 404 here; use `http://127.0.0.1:8082/` instead until it is upgraded.

If the binary path, the service name or the port is different, edit `DIR`, `SERVICE` or `HEALTH_URL` at the top of `deploy.sh` in step 2.

## 2. Install the deploy script

Copy the script to the server and make it root-owned. The `deploy` user must not be able to change it, because it runs as root:

```bash
# on your machine
scp scripts/deploy.sh you@your.vps:/tmp/deploy.sh

# on the VPS
sudo install -o root -g root -m 0755 /tmp/deploy.sh /opt/scribble/deploy.sh
rm /tmp/deploy.sh
```

The script needs `curl`, `tar` and `sha512sum`. These are installed on Debian and Ubuntu by default.

**Check:** install a release that has `/healthz`, for example:

```bash
sudo /opt/scribble/deploy.sh v0.0.9
```

It should end with the `/healthz` JSON and `deployed v0.0.9`. The previous binary is now in `/opt/scribble/scribble-server.prev`.

> The server runs its own copy of the script. If `scripts/deploy.sh` changes in the repo, repeat this step.

## 3. Create the deploy user and allow the script

If there is no `deploy` user yet, create it on the VPS:

```bash
sudo useradd --system --create-home --shell /bin/bash deploy
```

The user needs a real shell, because SSH runs the forced command in step 4 through it.

Then let `deploy` run this one script as root, without a password. Use a file of its own, so the rules for other projects stay as they are:

```bash
echo 'deploy ALL=(root) NOPASSWD: /opt/scribble/deploy.sh' | sudo tee /etc/sudoers.d/scribble-deploy
sudo chmod 0440 /etc/sudoers.d/scribble-deploy
sudo visudo -c
```

**Check:** `visudo -c` reports `parsed OK`.

## 4. Create the SSH key

On your own machine, create a key that is used only for scribble deploys. Don't add a passphrase, because GitHub has to use it without anyone typing one:

```bash
ssh-keygen -t ed25519 -N '' -C 'scribble deploy (GitHub Actions)' -f scribble_deploy_key
```

This creates `scribble_deploy_key` (private, goes to GitHub in step 6) and `scribble_deploy_key.pub` (public, goes on the server).

On the VPS, allow the public key, but only for running the deploy script. `tee -a` adds the line to the end of the file, so the keys of other projects stay:

```bash
sudo install -d -o deploy -g deploy -m 0700 /home/deploy/.ssh
sudo tee -a /home/deploy/.ssh/authorized_keys >/dev/null <<'EOF'
command="sudo /opt/scribble/deploy.sh \"$SSH_ORIGINAL_COMMAND\"",restrict PASTE-THE-CONTENTS-OF-scribble_deploy_key.pub-HERE
EOF
sudo chown deploy:deploy /home/deploy/.ssh/authorized_keys
sudo chmod 0600 /home/deploy/.ssh/authorized_keys
```

The new line is `command="…",restrict ssh-ed25519 AAAA… scribble deploy (GitHub Actions)`.

- `command=` means that whatever the client asks to run, the server runs scribble's `deploy.sh` with it as the tag. `deploy.sh` rejects anything that is not `vX.Y.Z`.
- `restrict` turns off port forwarding, agent forwarding and the terminal.

**Check:** from your machine, deploy the running version through the key:

```bash
ssh -i scribble_deploy_key deploy@your.vps v0.0.9
```

This should print the same output as in step 2. Then try something that is not a tag:

```bash
ssh -i scribble_deploy_key deploy@your.vps 'ls /'
```

This should print `usage: deploy.sh vX.Y.Z (got: 'ls /')` and do nothing else. If another project shares the user, run its own check too, to see that its key still deploys that project.

## 5. Get the server's host key

GitHub needs the server's host key, so that it can check it is connecting to your server. If another project already deploys to this server, you can reuse its known-hosts value. Otherwise, on your machine:

```bash
ssh-keyscan -t ed25519 your.vps > scribble_known_hosts
ssh-keygen -lf scribble_known_hosts
```

On the VPS, compare the fingerprint with the server's own:

```bash
ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub
```

**Check:** both fingerprints are the same. If SSH listens on a port other than 22, add `-p <port>` to `ssh-keyscan` and see the troubleshooting section.

## 6. Add the secrets on GitHub

In the scribble repository, go to **Settings → Environments → New environment** and create `production`. Optionally, under **Deployment branches and tags**, allow only tags that match `v*`.

Add three **environment secrets**:

| Secret | Value |
|---|---|
| `DEPLOY_HOST` | The VPS hostname or IP, for example `your.vps` |
| `DEPLOY_SSH_KEY` | The full contents of `scribble_deploy_key`, including the `BEGIN` and `END` lines |
| `DEPLOY_KNOWN_HOSTS` | The contents of `scribble_known_hosts` |

Then delete the private key from your machine, or move it somewhere safe:

```bash
rm scribble_deploy_key
```

## 7. Test the workflow by hand

Go to **Actions → Deploy → Run workflow**, enter the running version (for example `v0.0.9`) and run it.

**Check:** the job is green, and its log ends with `deployed v0.0.9`.

## 8. Release as usual

From now on, `make release` also deploys:

1. `make release` creates the GitHub release with your token. Publishing the release starts the `Deploy` workflow.
2. `deploy.sh` waits for the release files to finish uploading. It retries each file for up to 10 minutes, and gives up on a download attempt that stalls for 2 minutes.
3. The binary is replaced, the service restarts, and `/healthz` is checked.

**Pre-releases** (releases marked "This is a pre-release" on GitHub) are not deployed.

## 9. Schedule daily backups

`scribble-server backup` snapshots the database while the service runs, and deletes archives older than 7 days, but always keeps the newest one. The [README](../README.md#backups) describes the options and how to restore a backup.

Run it as the same user as the service, so it can read the database and its `.env`. Check the user:

```bash
systemctl show scribble-server -p User
```

An empty `User=` means root. The steps below assume root; for another user, use `sudo -u <user>` and `sudo crontab -u <user> -e` instead.

Take a first backup by hand:

```bash
sudo /opt/scribble/scribble-server backup
```

**Check:** it prints the archive path, for example `/opt/scribble/backups/scribble_20261019T030000Z.db.gz`. The archives go to a `backups` folder beside the database unless `BACKUP_DIR` is set in `/opt/scribble/.env`.

Print the crontab line and add it to root's crontab:

```bash
sudo /opt/scribble/scribble-server backup -cron-snippet
sudo crontab -e
```

The line looks like this. It runs at 03:00 server time and sends its output to the system log:

```cron
0 3 * * * cd /opt/scribble && /opt/scribble/scribble-server backup 2>&1 | logger -t scribble-backup
```

The line changes to `/opt/scribble` first, so the backup reads `/opt/scribble/.env` like the service does. Flags you give with `-cron-snippet` are copied into the line, for example `-retention-days 14`, with `-db` and `-dir` made absolute. `deploy.sh` replaces the binary at the same path, so the line keeps working after each release.

**Check:** the next morning, `journalctl -t scribble-backup` shows the new archive path, and `sudo ls /opt/scribble/backups` lists it.

The archives are on the same disk as the database. Copy them off the server too, for example with `rsync` from another machine.

## Troubleshooting

- **The workflow did not start after a release.** Releases created by a workflow with the built-in `GITHUB_TOKEN` don't start other workflows. `make release` uses your own token, so this only happens if releases are created another way. Run the workflow by hand.
- **`Host key verification failed`.** `DEPLOY_KNOWN_HOSTS` doesn't match the server. Repeat step 5. This also happens after reinstalling the VPS.
- **`Permission denied (publickey)`.** Check that each key in `/home/deploy/.ssh/authorized_keys` is on one line, and that the file is owned by `deploy` with mode `0600`. Run `sudo journalctl -u ssh -n 50` on the server for the reason.
- **Another project's deploy stopped working.** Its key line was probably removed in step 4. Check that `/home/deploy/.ssh/authorized_keys` still has one line per project, and add the missing one back.
- **`sudo: a password is required`.** The sudoers path must match exactly: `/opt/scribble/deploy.sh`.
- **SSH on a port other than 22.** Change the `ssh` line in the workflow to `ssh -p <port> -o BatchMode=yes deploy@"$HOST" "$TAG"`, and make sure `scribble_known_hosts` was made with `ssh-keyscan -p <port>`.
- **`rolling back`.** The new version started but `/healthz` didn't answer with 200. The previous binary is running again. Run `journalctl -u scribble-server -n 100` on the server for the reason. Then fix the problem and release again, or run the workflow by hand with the same tag.
- **No `scribble-backup` lines in the journal.** Check that `cron` runs (`systemctl status cron`) and that the line is in the crontab of the service's user (`sudo crontab -l` for root, `sudo crontab -u <user> -l` for another user). Run the line by hand to see its output.
- **`backup failed: database: … no such file or directory`.** The backup can't find the database. It reads `SQLITE_PATH` from `/opt/scribble/.env`, the same as the service. Check that the service and the backup run as the same user, and that the crontab line starts with `cd /opt/scribble &&`.
- **`curl: (22) … 404` after many retries.** The release has no file for this server's architecture. Check that `make release` built `linux_amd64` (or `linux_arm64` on an ARM VPS).
