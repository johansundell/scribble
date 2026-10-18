---
name: go-service-template
description: Square Moon template-service for Go daemons - kardianos/service lifecycle, store.Store with WASM SQLite, MySQL or FileMaker (OData), Gin routes with error-returning handlers, embedded or filesystem assets. Use when working in a repo that already follows template-service, or when explicitly asked to scaffold a new Square Moon Go service. Not general Go or general Gin guidance.
---

# Go Service Template Pattern

The code examples are illustrative, not compilable. The template-service repository is the reference implementation for names and APIs; the rules below are the standard it is held to. The important boundaries are:

```text
                    +-----------------------------+
                    |    main.go (Daemon CLI)     |
                    |      kardianos/service      |
                    +--------------+--------------+
                                   |
                    +--------------v--------------+
                    |   service.go (Lifecycle)    |
                    |     Start / Run / Stop      |
                    +--------------+--------------+
                                   |
                +------------------+------------------+
                |                                     |
     +----------v----------+           +--------------v--------------+
     |     store.Store     |           |          gin.Engine         |
     |     (Interface)     |           |      (router/router.go)     |
     +----------+----------+           +--------------+--------------+
                |                                     |
                |                      +--------------v--------------+
                |                      |     Middleware Pipeline     |
                |                      |   Wrap -> Auth -> Logger    |
                |                      +--------------+--------------+
     +----------+-----------+                         |
     |          |           |          +--------------v--------------+
+----v----+ +---v----+ +----v------+   |      handlers.Handler       |
| SQLite  | | MySQL  | | FileMaker |   |  func(*gin.Context) error   |
| Pure Go | | Driver | |  OData    |   +-----------------------------+
+---------+ +--------+ +-----------+
```

## Rules

1. **Service lifecycle**: `Start` validates configuration and initializes storage, routing, and the listener before returning startup success. Serving runs asynchronously. `Stop` triggers graceful shutdown with a five-second deadline for the HTTP server and up to 60 seconds for the log queue to drain, returning only after shutdown completes.
2. **Storage boundary**: Handlers and middleware depend on `store.Store`, not concrete database types.
3. **SQLite**: Use `github.com/ncruces/go-sqlite3` to keep builds CGO-free.
   **FileMaker**: Reach FileMaker Server only through `fmsodata` over `https://` with verified certificates (`FMS_CA_FILE` for a private CA); Basic auth is sent with every request. `FMS_INSECURE_SKIP_VERIFY=true` is the only exception: it disables verification and must log a warning on every start. The service never creates FileMaker tables; it checks them at startup.
4. **Error handlers**: Handlers return `error` and use `httperror.ReturnWithHTTPStatus` for HTTP failures.
5. **Routes**: Declare routes as `Route` values in `router.GetRoutes(handler)`; `router.NewRouter(cfg)` applies middleware and registers them.
6. **Authentication**: Protected routes fail closed. `router.NewRouter` rejects an empty `AUTH_TOKEN` when any route has `UseAuth: true`. When `AUTH_TOKEN` is unset, the service generates a random temporary token and logs it before building the router; real deployments must set `AUTH_TOKEN`. Compare tokens with `subtle.ConstantTimeCompare`.
7. **Assets**: Embedded assets are the default and require non-nil `cfg.Assets`. Filesystem mode loads assets and templates from paths relative to the executable directory.
8. **Ownership**: Close database handles on constructor failure and service shutdown. Do not use `log.Fatal` in reusable service or library code.

## Storage

Keep the storage contract small and interface-based. Every method takes a `context.Context`, so handlers can pass `c.Request.Context()` and a network-backed store stops work when the client leaves:

```go
type Store interface {
    Ping(ctx context.Context) error
    // GetLogs returns the entries with from <= CreatedAt < to, oldest first
    // (by CreatedAt, then ID), limited to page (store.Page{Limit, Offset}).
    GetLogs(ctx context.Context, from, to time.Time, page Page) ([]types.UsageLog, error)
    // LogRequests persists a batch, all or nothing where the backend supports it.
    // Wrap errors that retrying cannot fix with store.Permanent.
    LogRequests(ctx context.Context, entries []types.UsageLog) error
    Close() error
}
```

`STORAGE` (`sqlite`, `mysql` or `filemaker`) selects the backend. Each backend has a constructor that opens and owns its connection (`store.NewSQLite(path)`, `store.NewMySQL(settings.MySQL)`, `store.NewFileMaker(ctx, settings.FileMaker)`); the service looks the constructor up by `STORAGE` in one map, checked by a test against `types.StorageBackends`, opens the configured store, `defer`s `Close()` immediately, then pings it (using `FMS_TIMEOUT` for FileMaker, or a short deadline otherwise) before building the router.

`store.FileMakerStore` keeps logs in a FileMaker table created by a FileMaker developer. Its constructor fails unless a `$select` of every field with `$top=0` succeeds, so setup mistakes surface at deploy time. `LogRequests` sends one `$batch` change set with `Prefer: return=minimal`; 4xx responses (except 408 and 429) are wrapped with `store.Permanent`. `CreatedAt` is written as UTC without an offset and read back as UTC wall-clock time.

Store timestamps in UTC so ranges compare correctly on any server. SQLite keeps `created_at` as text, so write it in a fixed-width UTC layout; the driver's default RFC3339Nano varies in length and does not sort as a string.

Constructors should close an opened handle if schema creation fails. Set SQLite pragmas in the DSN, not with `db.Exec`: the driver then applies them to every pooled connection, and a bad pragma surfaces as an error on the first statement:

```go
func NewSQLite(file string) (*SQLStore, error) {
    db, err := sql.Open("sqlite3", "file:"+file+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
    if err != nil {
        return nil, err
    }
    if _, err = db.Exec(createRequestLogsTable); err != nil {
        _ = db.Close()
        return nil, err
    }
    return &SQLStore{db: db, timeArg: func(t time.Time) any {
        return t.UTC().Format("2006-01-02T15:04:05.000000Z07:00")
    }}, nil
}
```

## Typed HTTP Errors

`statusError` implements `Unwrap`, so status extraction must use `errors.As`. A direct type assertion loses the status after idiomatic `%w` wrapping. `ReturnWithHTTPStatus` returns `statusError` by value; a `*statusError` would not match these targets:

```go
func HTTPStatus(err error) int {
    var statusErr statusError
    if errors.As(err, &statusErr) {
        return statusErr.status
    }
    return http.StatusInternalServerError
}

func StatusText(err error) string {
    var statusErr statusError
    if errors.As(err, &statusErr) {
        return http.StatusText(statusErr.status)
    }
    return http.StatusText(http.StatusInternalServerError)
}
```

Test both direct and wrapped errors:

```go
baseErr := errors.New("user not found")
err := fmt.Errorf("load user: %w", ReturnWithHTTPStatus(baseErr, http.StatusNotFound))
```

## Routes and Middleware

`router.Config` carries the dependencies and settings required to construct the router:

```go
type Config struct {
    Handler  *handlers.Handler
    LogSink  LogSink // Required when any route has UseLogger; logqueue.Queue
    Settings types.AppSettings
    Assets   fs.FS
    Version  string
    Logger   Logger // Optional: defaults to standard logger when nil
}
```

Middleware and the log queue log through one leveled interface, `logging.Logger` (`router.Logger` is an alias). The service passes an adapter over the `kardianos/service` logger, falling back to `logging.Std` before one exists, so errors reach the system log at the right level:

```go
// package logging
type Logger interface {
    Infof(format string, v ...interface{})
    Warningf(format string, v ...interface{})
    Errorf(format string, v ...interface{})
}
```

The route collection in `router/routes.go` is the extension point:

```go
func GetRoutes(handler *handlers.Handler) Routes {
    return Routes{
        {
            Name: "HealthCheck", Method: "GET", Pattern: "/",
            HandlerFunc: handler.HealthCheck,
        },
        {
            Name: "Example", Method: "GET", Pattern: "/example",
            HandlerFunc: handler.Example,
            UseAuth: true,
            UseLogger: true,
        },
    }
}
```

With `DEBUG=true` (`cfg.Settings.Debug`), `NewRouter` logs the route table and adds `router.AccessLog`, one line per request, through the injected logger rather than Gin's stdout debug mode, so the output reaches the service log.

`router.NewRouter(cfg)` validates that `cfg.Handler` is provided, obtains routes via `router.GetRoutes(cfg.Handler)`, validates that every `UseAuth` route has a token and every `UseLogger` route has a log sink (returning an error instead of registering an unsafe route), applies authentication outside the logger, then registers `WrapHandler`. Request order is `WrapHandler -> Auth -> Logger -> handler`:

```go
if cfg.Handler == nil {
    return nil, errors.New("handler must be provided")
}

for _, route := range GetRoutes(cfg.Handler) {
    if route.UseAuth && cfg.Settings.AuthToken == "" {
        return nil, fmt.Errorf("AUTH_TOKEN must be configured for route %q", route.Name)
    }
    if route.UseLogger && cfg.LogSink == nil {
        return nil, fmt.Errorf("log sink must be configured for logged route %q", route.Name)
    }

    fn := route.HandlerFunc
    if route.UseLogger {
        fn = LoggerMiddleware(cfg.LogSink, l)(fn)
    }
    if route.UseAuth {
        fn = AuthMiddleware(cfg.Settings.AuthToken, l)(fn)
    }
    router.Handle(route.Method, route.Pattern, WrapHandler(fn, cfg.Version))
}
```

The wrapper converts returned errors into HTTP responses and sets the `X-Version` header:

```go
func WrapHandler(inner HandlerFuncWithError, version string) gin.HandlerFunc {
    return func(c *gin.Context) {
        if version != "" {
            c.Header("X-Version", version)
        }
        if err := inner(c); err != nil {
            c.String(httperror.HTTPStatus(err), httperror.StatusText(err))
        }
    }
}
```

### Authentication

Middleware must never turn a protected route into a public route because configuration is missing. The service guarantees a token before building the router (`ensureAuthToken`), and `NewRouter` rejects protected routes without one; the middleware should also fail closed if called directly:

```go
if authToken == "" {
    return httperror.ReturnWithHTTPStatus(
        errors.New("authentication is not configured"),
        http.StatusInternalServerError,
    )
}
```

Accept the configured authorization format consistently, reject missing or invalid credentials with 401, and use `subtle.ConstantTimeCompare` for the final comparison. Log rejected requests through the injected logger without the token value.

### LoggerMiddleware

The logger middleware must cap and restore the request body so JSON binding still works, and wrap Gin's writer to capture response bytes. Override both `Write` and `WriteString` on a pointer receiver, or responses written as strings are missing from the log:

```go
const maxRequestBodyBytes = 1 << 20

type bodyLogWriter struct {
    gin.ResponseWriter
    body *bytes.Buffer
}

func (w *bodyLogWriter) Write(b []byte) (int, error) {
    w.body.Write(b)
    return w.ResponseWriter.Write(b)
}

func (w *bodyLogWriter) WriteString(s string) (int, error) {
    w.body.WriteString(s)
    return w.ResponseWriter.WriteString(s)
}
```

```go
c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBodyBytes)
requestBody, err := io.ReadAll(c.Request.Body)
if err != nil {
    var tooLarge *http.MaxBytesError
    if errors.As(err, &tooLarge) {
        return httperror.ReturnWithHTTPStatus(err, http.StatusRequestEntityTooLarge)
    }
    return err
}
c.Request.Body = io.NopCloser(bytes.NewReader(requestBody))

writer := &bodyLogWriter{
    ResponseWriter: c.Writer,
    body:           bytes.NewBuffer(nil),
}
c.Writer = writer

handlerErr := inner(c)
status := writer.Status()
if handlerErr != nil {
    // WrapHandler writes the error response after this returns.
    status = httperror.HTTPStatus(handlerErr)
}
// Build a types.UsageLog from requestBody, writer.body, status, endpoint and
// handlerErr, and hand it off without blocking.
sink.Enqueue(entry)
return handlerErr
```

Apply auth outside the logger so unauthorized request bodies are never persisted.

The middleware never writes to the store itself: a slow or unavailable store must not slow down or fail a request. `logqueue.Queue` writes entries from one background worker:

- A bounded queue (1000 entries); when it is full, new entries are dropped with at most one warning per minute that carries the count.
- Batches of up to 50 entries or 1 second, written with `Store.LogRequests`: one SQL transaction per batch, or one `$batch` request for FileMaker. Each written batch is logged only with `DEBUG=true`; drops and failures are always logged.
- A failed batch is retried with doubling backoff for about 30 seconds, then dropped and logged; errors wrapped with `store.Permanent` are dropped at once.
- The service closes the queue after the HTTP server has shut down, draining it for up to 5 more seconds before the store is closed; entries left after that are counted and logged as an error. The drain deadline overrides the retry schedule.

## Service Lifecycle

The service worker must report initialization failures to `Start` instead of silently logging them:

```go
func (p *program) Start(s service.Service) error {
    loadSettings()
    if err := settings.Validate(); err != nil {
        return err
    }

    p.exit = make(chan struct{})
    p.done = make(chan error, 1)
    startup := make(chan error, 1)
    go func() {
        err := p.run(startup)
        if err != nil {
            log.Printf("service stopped: %v", err)
        }
        // Unblocks Start when run returns before signalling startup.
        select {
        case startup <- err:
        default:
        }
        p.done <- err
    }()
    return <-startup
}

func (p *program) Stop(s service.Service) error {
    close(p.exit)
    return <-p.done
}

func (p *program) run(startup chan<- error) error {
    st, err := openStore() // store constructor chosen by STORAGE
    if err != nil {
        return err
    }
    defer st.Close()

    pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    err = st.Ping(pingCtx)
    cancel()
    if err != nil {
        return err
    }
    ensureAuthToken() // random temporary token when AUTH_TOKEN is unset

    router, err := buildRouter(st) // repository handler + router.NewRouter wiring
    if err != nil {
        return err
    }
    ln, err := net.Listen("tcp", settings.Port)
    if err != nil {
        return err
    }

    srv := &http.Server{Handler: router}
    serveErr := make(chan error, 1)
    go func() { serveErr <- srv.Serve(ln) }()
    startup <- nil

    select {
    case err := <-serveErr:
        return err
    case <-p.exit:
    }
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    return srv.Shutdown(ctx)
}
```

`buildRouter` is a descriptive placeholder, not a repository API. The real implementation must propagate database, router, and listener errors, close resources on every exit path, and avoid `log.Fatal` in the worker.

## Adding an Endpoint

1. Create or update a handler in `handlers/` with signature `func (h *Handler) Action(c *gin.Context) error`.
2. Return `httperror.ReturnWithHTTPStatus` for expected HTTP failures.
3. Add a `Route` entry to `router.GetRoutes(handler)` in `router/routes.go` and choose `UseAuth` and `UseLogger` explicitly.
4. Add focused handler and route tests. Mock `store.Store` where possible.
5. Run `go test ./...` and `go build ./...`.

## Filesystem Assets and Templates

When filesystem mode is enabled, package the `assets` and `tmpl` directories beside the compiled binary. Resolve paths from `utils.GetBinaryBasePath()` rather than the process working directory. Embedded mode uses `embed.FS` and remains portable as a single binary.

## Docker Standard

Use a multi-stage CGO-free build. Copy the binary, `assets`, and `tmpl` into the same runtime directory, run as a non-root user, and listen on a fixed container port (`ENV PORT=:8080`, matching `EXPOSE` and the health check); map a different host port rather than changing `PORT` in the container. Keep the image health check pointed at the public health endpoint. When deploying with SQLite, mount a named volume (not a host folder, which the non-root user usually can't write) at the `SQLITE_PATH` directory, e.g. `data:/app/data`, so the database and its WAL and SHM files persist.
