# scribble-server

A robust Go-based service template designed for quick bootstrapping of web services. It includes built-in support for system service management, request logging to SQLite, MySQL or FileMaker, authentication, and Docker deployment.

## API Endpoints

Every response carries an `X-Version` header with the build version, and an `X-Scribble-API` header with the API level (see the README's API section). Errors are returned as plain text with the HTTP status text as the body (for example `Not Found`). A request that runs longer than `TIMEOUT` seconds gets **503** with the body `Timeout`.

### Public Endpoints

- **GET /**
  - Health check. Pings the storage backend and returns an HTML page, or JSON `{"title", "name", "version", "dbStatus"}` when the request's `Accept` header prefers JSON (for example `application/json` or `application/json, text/plain`). Browsers and requests without an `Accept` header get HTML.
  - `dbStatus` is `OK`, or the storage error. The status is **200** when storage answers and **503** when it doesn't, so Docker's `HEALTHCHECK` and load balancers see the service as unhealthy.

- **GET /ping/:argument**
  - Echo endpoint. Returns `{"result": "<argument>"}`. `/ping/notfound` returns **404** (an example of an error response).
  - Logged to the database (see [Request logging](#request-logging)).

- **GET /assets/\***
  - Static files (CSS, images), embedded in the binary or read from disk with `USE_FILE_SYSTEM=true`.

### Protected Endpoints

These endpoints require an `Authorization` header with the configured `AUTH_TOKEN` (e.g., `Authorization: Bearer <token>` or `Authorization: <token>`). A missing or wrong token gets **401**.

- **POST /pong**
  - Echo endpoint. Accepts a JSON **object** and returns `{"message": <input>}`. Anything else (invalid JSON, an array, an empty body) gets **400**.
  - Logged to the database.

- **GET /logs/:from/:to**
  - Retrieve usage logs within a date range, oldest first (by time, then ID), one page at a time:

    ```json
    {
      "entries": [{"id": 1, "status": 200, "method": "GET", "endpoint": "...", "created_at": "2026-09-30T10:15:00Z", "...": "..."}],
      "next": "/logs/2026-09-30/2026-09-30?limit=1000&offset=1000"
    }
    ```

  - `?limit=` sets the page size (default **1000**, at most **10000**) and `?offset=` skips that many entries (default 0). Other values get **400**. Follow `next` until it is `null` to read the whole range; an empty range returns `{"entries": [], "next": null}`.
  - Logs are written in the background, so the newest entries can take up to about a second to appear.
  - `:from` and `:to` are dates in `YYYY-MM-DD` format (anything else gets **400**) and mean whole **UTC** days: from `:from` 00:00Z up to, not including, the day after `:to`. Timestamps are stored in UTC.

### Request logging

`GET /ping/:argument` and `POST /pong` are logged to the storage backend: method, endpoint (including query strings), status, error, and the full request and response bodies. **Warning:** Because the full endpoint with query strings is logged, avoid passing sensitive data (like tokens) in URL parameters to prevent them from being stored in the database. Bodies over **1 MiB** on these routes are rejected with **413** and not logged. In `GET /logs`, a body that is valid JSON appears as JSON; any other body (plain text or truncated data) appears as a JSON string, and an empty body as `{}`. For requests that end in an error, the response body is written after logging, so the entry has `"response": null` with the status and error message in their own fields.

Entries are written in the background (see Features), so logging never slows a request, but entries **can be lost**: when more than 1,000 are waiting (a slow or unreachable database), new ones are dropped; a batch that keeps failing is dropped after about 60 seconds of retries; and at shutdown, entries not written within 5 seconds are dropped. Each of these is logged as a warning or error with the number of entries.

`/ping` is public, so anyone who can reach the service can add rows to the log table. Put the service behind a firewall or proxy, or turn `UseLogger` off for public routes in `router/routes.go`, if that matters for your deployment.

### Authentication token

If `AUTH_TOKEN` is not set, the service generates a random token when it starts and logs it as a warning (`AUTH_TOKEN is not set; using temporary token for this run: ...`). The token changes on every restart and appears in the service log, so set `AUTH_TOKEN` for any real deployment.

## Service Management

The application can be installed as a system service.

```bash
# Install the service
./scribble-server -service install

# Start the service
./scribble-server -service start

# Stop the service
./scribble-server -service stop

# Uninstall the service
./scribble-server -service uninstall
```

## Features

- **Web Server**: Built with [Gin](https://github.com/gin-gonic/gin) for high performance.
- **Service Management**: Can be installed and managed as a system service (Windows Service, Systemd, etc.) using [kardianos/service](https://github.com/kardianos/service).
- **Database Support**: Request logs in SQLite, MySQL or FileMaker (see [FileMaker storage](#filemaker-storage)).
- **Authentication**: Simple token-based authentication for protected routes.
- **Docker Ready**: Includes `Dockerfile` and `docker-compose.yml` for easy containerization.
- **Asset Management**: Supports embedding assets or serving from the file system.
- **Logging**: Request logging to database. Entries are written in the background in batches, so a slow database never slows down requests; `GET /logs` can be up to about a second behind, and pending entries are written when the service stops.

## Getting Started

### Prerequisites

- [Go](https://golang.org/dl/) 1.27.1 or higher
- [Make](https://www.gnu.org/software/make/) (optional, for build scripts)
- [Docker](https://www.docker.com/) (optional, for containerized run)

### Installation

Clone the repository:

```bash
git clone https://github.com/johansundell/scribble.git
cd scribble-server
```

### Creating a new service from the template

Rename the Go module and the service. The service name is used for the binary, the system service, the default SQLite file, the Docker image and the compose service. Run this in the fresh clone (GNU `sed`; on macOS use `sed -i ''`):

```bash
NEW_MODULE=github.com/acme/billing-service   # Go module path of the new service
NEW_NAME=billing-service                     # service, binary and Docker name
NEW_ACCOUNT=acme                             # GitHub account for `make release`

OLD_MODULE=github.com/johansundell/scribble
git grep -lz "$OLD_MODULE" | xargs -0 sed -i "s#$OLD_MODULE#$NEW_MODULE#g"
git grep -lz scribble-server -- ':!.agents' | xargs -0 sed -i "s#scribble-server#$NEW_NAME#g"
sed -i "s#^GHACCOUNT := .*#GHACCOUNT := $NEW_ACCOUNT#" Makefile

go build ./... && go test ./...
```

The first command rewrites the module path in `go.mod`, the imports and the docs; the second renames the service everywhere else (`main.go`'s `nameOfService`, the `Makefile`, `Dockerfile`, `docker-compose.yml`, `.gitignore` and the READMEs). `.agents/skills` is left alone, because the skill describes the template. Then start a fresh history if you like (`rm -rf .git && git init`), and set the version in the `Makefile`.

### Running Locally

You can run the service directly using Go:

```bash
go run .
```

Or build it using Make:

```bash
make build
./scribble-server
```

The cross-platform targets (`make compile`, `make dist`, `make release`) need `gox` and `github-release`. Install them once with `make deps`; they go into `$(go env GOPATH)/bin`, which must be on your `PATH`.

The service resolves paths relative to **its binary's folder**: the `assets` and `tmpl` folders when `USE_FILE_SYSTEM=true`, a `.env` file (after the current directory), and the default `SQLITE_PATH`. `go run .` builds the binary in a temporary Go folder, which has two effects:

- **`USE_FILE_SYSTEM=true` doesn't work with `go run .`**: the assets and templates aren't found, so `GET /` returns 500 and `/assets/...` returns 404. Use embedded assets (the default), or build first with `go build` or `make build` and run the binary from the repo, as above.
- **The default SQLite file lands in that temporary folder** and is gone after the next build. With `go run .`, set `SQLITE_PATH`, for example `SQLITE_PATH=./scribble-server.db go run .` (`*.db` is gitignored).

### Running with Docker

To run the service using Docker Compose:

```bash
make docker-run
```

This runs `docker compose up --build` while automatically injecting the correct `VERSION` from the `Makefile`.

This will start the service on port 8080, with the SQLite database (including its WAL and SHM files) in the named Docker volume `data`, mounted at `/app/data`. The volume keeps the data across `docker compose down` and rebuilds; **`docker compose down -v` deletes it**.

The service runs as a non-root user, and a named volume gets the right ownership automatically. A host folder like `./data` usually belongs to your own user and makes SQLite fail with `permission denied`; if you need one, `chown` it to the container user first (`docker compose run --rm --entrypoint id scribble-server` shows the uid and gid).

To copy the database out, for a backup or to inspect it:

```bash
docker compose cp scribble-server:/app/data/scribble-server.db ./scribble-server.db
```

Inside the container the service always listens on **8080** (the image sets `PORT=:8080`), which the image's `EXPOSE` and health check rely on. Choose the port on the host instead: `HOST_PORT=9090 docker compose up`, or `docker run -p 9090:8080 ...`. Don't set `PORT` for the container. The health check calls `GET /`, so the container turns unhealthy when the storage backend is unreachable.

### Behind nginx

[`docs/nginx/notes.sudde.net.conf`](nginx/notes.sudde.net.conf) is an example reverse proxy that serves the service over HTTPS at `notes.sudde.net`, with the service bound to `127.0.0.1:8082` (`PORT=127.0.0.1:8082`, or `HOST_PORT=127.0.0.1:8082` with Docker). It raises nginx's 1 MB body limit, because notes may be up to 4 MB.

## Configuration

The application is configured via environment variables. You can set these in a `.env` file in the root directory. Environment variables explicitly set in the system or terminal take precedence over values in the `.env` file.

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `DEBUG` | bool | `false` | Log the route table at startup, one access log line per request (method, path, status, duration, client IP), and each written batch of request log entries, through the service log. For troubleshooting; leave it off in normal use. |
| `PORT` | string | `:8080` | The port the server listens on. |
| `USE_FILE_SYSTEM` | bool | `false` | If true, serves assets and templates from the `assets` and `tmpl` folders next to the binary (edit them without rebuilding). If false, uses the embedded copies. Doesn't work with `go run .` (see [Running Locally](#running-locally)). |
| `TIMEOUT` | int | `15` | Request timeout in seconds. |
| `STORAGE` | string | `sqlite` | Storage backend for request logs: `sqlite`, `mysql` or `filemaker`. |
| `SQLITE_PATH` | string | `<binary dir>/<nameOfService>.db` | Path to SQLite database file (`STORAGE=sqlite`). Set it when using `go run .`, whose binary dir is temporary. |
| `BACKUP_DIR` | string | `backups` beside the database | Where `scribble-server backup` stores its `scribble_<timestamp>.db.gz` archives. The `-dir` flag overrides it. |
| `RETENTION_DAYS` | int | `7` | After each backup, `scribble-server backup` deletes archives older than this many days. The newest archive is always kept, however old. The `-retention-days` flag overrides it. |
| `AUTH_TOKEN` | string | random per start | Token required for protected endpoints. When unset, a temporary token is generated and logged (see [Authentication token](#authentication-token)). |
| `WEB_PIN` | string | - | The PIN for the web app at `/`: 6–12 digits. **Unset means the web app is off**: its paths answer 404 and `/` redirects to `/healthz`. The JSON API never accepts it. |
| `SESSION_SECRET` | string | - | The key that signs the web app's session cookie: at least 32 characters, and not equal to `AUTH_TOKEN`. Required when `WEB_PIN` is set. Generate it with `openssl rand -base64 33`. Changing it ends every session. |
| `WEB_SESSION_TIMEOUT` | duration | `168h` | How long a web login lasts (7 days). The expiry is fixed at login and never extended. |
| `TRUSTED_PROXIES` | string | none | Comma-separated IPs or CIDRs whose `X-Forwarded-For` is believed, for example `127.0.0.1` for nginx on the same host. With none, the client address is the TCP peer. It keys the web app's per-address PIN limit. |
| `COOKIE_SECURE` | bool | `true` | Marks the session cookie `Secure` (HTTPS only). Set `false` only for local testing over plain HTTP. |
| `MYSQL_USERNAME` | string | - | MySQL username (required when `STORAGE=mysql`, as are `MYSQL_HOST` and `MYSQL_DATABASE`). |
| `MYSQL_PASSWORD` | string | - | MySQL password. |
| `MYSQL_HOST` | string | - | MySQL host address. |
| `MYSQL_PORT` | string | `3306` | MySQL port. |
| `MYSQL_DATABASE` | string | - | MySQL database name. |
| `FMS_HOST` | string | - | FileMaker Server URL; must start with `https://` (required when `STORAGE=filemaker`, as are `FMS_DATABASE`, `FMS_USERNAME` and `FMS_PASSWORD`). |
| `FMS_DATABASE` | string | - | FileMaker file (database) name. |
| `FMS_USERNAME` | string | - | FileMaker account with the `fmodata` extended privilege. |
| `FMS_PASSWORD` | string | - | Password for that account. |
| `FMS_TIMEOUT` | duration | `10s` | Timeout for each FileMaker request, and for the startup check. |
| `FMS_LOG_TABLE` | string | `Logs` | FileMaker table for request logs. |
| `FMS_CA_FILE` | string | - | PEM file with extra CA certificates to trust, for a server with a private CA. |
| `FMS_INSECURE_SKIP_VERIFY` | bool | `false` | Skip certificate verification. Logs a warning on every start; prefer `FMS_CA_FILE`. |
| `NOTES_REPLICA` | string | empty | `filemaker` turns on the FileMaker replica of the notes (see [FileMaker replica of the notes](#filemaker-replica-of-the-notes)). Empty means no replica. Needs `STORAGE=sqlite` and the `FMS_HOST`, `FMS_DATABASE`, `FMS_USERNAME` and `FMS_PASSWORD` settings. |
| `FMS_NOTES_TABLE` | string | `Notes` | The replica of the notes. |
| `FMS_PENDING_TABLE` | string | `PendingEdits` | Edits made in FileMaker, waiting for the sync. |
| `FMS_INFO_TABLE` | string | `ScribbleInfo` | One record holding the schema version. |
| `NOTES_REPLICA_INTERVAL` | duration | `30s` | How often the sync runs. It also runs about 2 seconds after any note write. |

## FileMaker storage

With `STORAGE=filemaker`, request logs are written to a FileMaker table through the FileMaker OData API. The service never creates or changes the table: a FileMaker developer sets it up once, and at startup the service checks that it can read the table and every field. A missing table or field, a wrong password or an unreachable server stops startup with FileMaker's error.

Create the table (default name `Logs`, see `FMS_LOG_TABLE`) with these fields:

| Field | Type | Notes |
|-------|------|-------|
| `ID` | Number | Auto-enter serial number, unique. Returned as `id` by `GET /logs`. |
| `Status` | Number | |
| `Method` | Text | |
| `Error` | Text | |
| `Endpoint` | Text | |
| `CreatedAt` | Timestamp | Stored in **UTC** without a time zone. Add a calculation field if people browsing the file want local time. |
| `Request` | Text | Turn indexing off (bodies up to 1 MiB). |
| `Response` | Text | Turn indexing off. |

The account needs the `fmodata` extended privilege and create and view access to the table.

**Connection.** `FMS_HOST` must use `https://`, because the account's password is sent with every request. Certificates are verified; for a server with a private CA, point `FMS_CA_FILE` at the CA's PEM file. `FMS_INSECURE_SKIP_VERIFY=true` turns verification off and logs a warning on every start.

**Writes.** Log entries are sent in batches as one OData `$batch` request each, asking FileMaker not to send the records back (`Prefer: return=minimal`) to save the server's data-transfer allowance. A rejected batch (a 4xx response, for example a changed field) is dropped and logged at once; network errors and 5xx responses are retried.

**Testing against a real server.** `go test -tags filemaker ./store` runs an integration test when `FMS_TEST_HOST`, `FMS_TEST_DATABASE`, `FMS_TEST_USERNAME` and `FMS_TEST_PASSWORD` (and optionally `FMS_TEST_CA_FILE`) are set. It uses its own variables so it never touches a service's database, and creates and deletes a temporary `LogsTest_<timestamp>` table; the account needs schema privileges for that.

## FileMaker replica of the notes

With `NOTES_REPLICA=filemaker`, a background sync keeps a copy of the notes in FileMaker, both ways (#66, [ADR 10](adr/0010-sqlite-master-filemaker-replica.md)). SQLite stays the master, so `STORAGE` must be `sqlite`; request logs are unaffected. The replica uses the same `FMS_*` connection settings as FileMaker storage.

The tables, their fields and the account's privileges are in [docs/filemaker/notes.md](filemaker/notes.md). A FileMaker developer sets them up once; the service never creates them. A missing or wrong setup logs an error and turns the replica off, and the server still starts.

`/healthz` adds a `replica` object, `{"status": "ok" | "behind" | "unreachable" | "off", "pending_push": <notes not yet pushed>, "last_sync": "<UTC>"}`, with a `reason` when it is unreachable or off. It still answers 200 while FileMaker is down, because SQLite is healthy. `scribble-server replica resync` pushes every note again, for a FileMaker file that was replaced or emptied.


