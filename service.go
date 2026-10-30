package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/johansundell/scribble/auth"
	"github.com/johansundell/scribble/handlers"
	"github.com/johansundell/scribble/logging"
	"github.com/johansundell/scribble/logqueue"
	"github.com/johansundell/scribble/replica"
	"github.com/johansundell/scribble/router"
	"github.com/johansundell/scribble/store"
	"github.com/johansundell/scribble/types"
	"github.com/kardianos/service"
)

var logger service.Logger

type program struct {
	exit     chan struct{}
	done     chan struct{} // closed when run returns; runErr holds its result
	runErr   error
	stopOnce sync.Once
	// failed receives the error when the HTTP server stops on its own after a
	// successful start, so main can exit and let the service manager restart us.
	failed chan error
}

func newProgram() *program {
	return &program{failed: make(chan error, 1)}
}

// injectable constructors so tests can mock storage initialization and listening
var newSQLiteStore = func(path string) (store.Store, error) {
	s, err := store.NewSQLite(path)
	if err != nil {
		return nil, err
	}
	return s, nil
}
var newMySQLStore = func(cfg types.MySQLSettings) (store.Store, error) {
	s, err := store.NewMySQL(cfg)
	if err != nil {
		return nil, err
	}
	return s, nil
}
var newFileMakerStore = func(ctx context.Context, cfg types.FileMakerSettings) (store.Store, error) {
	s, err := store.NewFileMaker(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if s.CreatedLogTable() {
		logWarning("FMS_CREATE_LOG_TABLE: created FileMaker log table %s in %s", cfg.LogTable, cfg.Database)
	}
	return s, nil
}
var newReplicaClient = func(cfg types.FileMakerSettings) (replica.Client, error) {
	c, err := store.NewFileMakerClient(cfg)
	if err != nil {
		return nil, err
	}
	return c, nil
}
var netListen = net.Listen

func (p *program) Start(s service.Service) error {
	loadSettings()
	if err := settings.Validate(); err != nil {
		logError("invalid configuration: %v", err)
		return err
	}
	if service.Interactive() {
		logInfo("Running in terminal.")
	} else {
		logInfo("Running under service manager.")
	}
	return p.startWorker()
}

// startWorker runs the service worker in the background. Start should not
// block while the service is serving requests, but it must report
// initialization failures to the service manager.
func (p *program) startWorker() error {
	p.exit = make(chan struct{})
	p.done = make(chan struct{})
	startup := make(chan error, 1)
	go func() {
		p.runErr = p.run(startup)
		close(p.done)
	}()
	return <-startup
}

func (p *program) run(startup chan<- error) error {
	logInfo("I'm running %v, with version %v.", service.Platform(), Version)

	st, pingTimeout, err := openStore()
	if err != nil {
		logError("failed to initialize %s storage: %v", settings.Storage, err)
		startup <- err
		return err
	}
	defer st.Close()

	pingCtx, cancelPing := context.WithTimeout(context.Background(), pingTimeout)
	err = st.Ping(pingCtx)
	cancelPing()
	if err != nil {
		logError("database ping failed: %v", err)
		startup <- err
		return err
	}
	ensureAuthToken()

	// Request logs are written in the background. The deferred Close runs after
	// the HTTP server has shut down, drains the queue for up to 5 more seconds
	// and runs before the store is closed.
	logQueue := logqueue.New(st, appLogger(), settings.Debug)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		logQueue.Close(ctx)
	}()

	webAuth, err := newWebAuth(settings.Web)
	if err != nil {
		logError("failed to set up web login: %v", err)
		startup <- err
		return err
	}
	if webAuth != nil {
		logInfo("web app is on at /")
	}

	// Stopped before the store is closed (defers run in reverse).
	syncer, stopReplica := startReplica(st)
	defer stopReplica()
	handlerOpts := []handlers.Option{handlers.WithAuth(webAuth)}
	if syncer != nil {
		handlerOpts = append(handlerOpts, handlers.WithReplica(syncer))
	}

	handler, err := handlers.NewHandler(st, settings.UseFileSystem, embeddedTemplates, nameOfService, Version, handlerOpts...)
	if err != nil {
		logError("failed to create handlers: %v", err)
		startup <- err
		return err
	}

	if settings.Storage != types.StorageSQLite {
		logWarning("STORAGE=%s: the notes API needs STORAGE=sqlite and will answer 501", settings.Storage)
	}

	routerEngine, err := router.NewRouter(router.Config{
		Handler:  handler,
		LogSink:  logQueue,
		Auth:     webAuth,
		Settings: settings,
		Assets:   embeddedAssets,
		Version:  Version,
		Logger:   appLogger(),
	})
	if err != nil {
		logError("failed to create router: %v", err)
		startup <- err
		return err
	}
	timeoutHandler := http.TimeoutHandler(routerEngine, settings.Timeout, "Timeout")
	// Set out here too, so a timed-out request's 503 carries them.
	apiLevel := strconv.Itoa(router.APILevel)
	rootHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Version != "" {
			w.Header().Set("X-Version", Version)
		}
		w.Header().Set(router.APIHeader, apiLevel)
		timeoutHandler.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Handler: rootHandler,
		Addr:    settings.Port,
	}
	listener, err := netListen("tcp", settings.Port)
	if err != nil {
		logError("failed to listen on %s: %v", settings.Port, err)
		startup <- err
		return err
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(listener) }()
	startup <- nil

	select {
	case err := <-serveErr:
		logError("HTTP server stopped: %v", err)
		select {
		case p.failed <- err:
		default:
		}
		return err
	case <-p.exit:
	}

	// The shutdown deadline is shorter than TIMEOUT (default 15s), so a slow
	// request can still be cut off when the service stops.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logError("HTTP server shutdown failed: %v", err)
		return err
	}
	return nil
}

func wrapStore[T store.Store](s T, err error) (store.Store, error) {
	if err != nil {
		return nil, err
	}
	return s, nil
}

// storeOpeners opens each STORAGE backend. Keep it in step with
// types.StorageBackends (TestStoreOpenersMatchStorageBackends).
var storeOpeners = map[string]func() (store.Store, time.Duration, error){
	types.StorageSQLite: func() (store.Store, time.Duration, error) {
		s, err := newSQLiteStore(settings.SqlitePath)
		return s, 5 * time.Second, err
	},
	types.StorageMySQL: func() (store.Store, time.Duration, error) {
		s, err := newMySQLStore(settings.MySQL)
		return s, 5 * time.Second, err
	},
	types.StorageFileMaker: func() (store.Store, time.Duration, error) {
		fm := settings.FileMaker
		if fm.InsecureSkipVerify {
			logWarning("FMS_INSECURE_SKIP_VERIFY is set: the FileMaker Server certificate is NOT verified, so credentials can be intercepted. Use FMS_CA_FILE instead.")
		}
		ctx, cancel := context.WithTimeout(context.Background(), fm.Timeout)
		defer cancel()
		s, err := newFileMakerStore(ctx, fm)
		return s, fm.Timeout, err
	},
}

// startReplica starts the FileMaker replica's sync (#66, ADR 10) when
// NOTES_REPLICA is set, and returns it with a func that stops it. It never
// fails startup: SQLite is the master, so a broken FileMaker setup only
// turns the replica off, which /healthz shows.
func startReplica(st store.Store) (*replica.Syncer, func()) {
	r := settings.Replica
	if !r.Enabled() {
		return nil, func() {}
	}
	db, ok := st.(replica.Store)
	if !ok { // Validate requires STORAGE=sqlite
		logError("NOTES_REPLICA=filemaker needs STORAGE=sqlite; the replica is off")
		return nil, func() {}
	}
	if settings.FileMaker.InsecureSkipVerify {
		logWarning("FMS_INSECURE_SKIP_VERIFY is set: the FileMaker Server certificate is NOT verified, so credentials can be intercepted. Use FMS_CA_FILE instead.")
	}
	client, err := newReplicaClient(settings.FileMaker)
	syncer := replica.New(client, db, replica.Config{
		Tables:   replica.Tables{Notes: r.NotesTable, Pending: r.PendingTable, Info: r.InfoTable},
		Interval: r.Interval,
		Logger:   appLogger(),
	})
	if err != nil {
		syncer.Disable(fmt.Errorf("connect to FileMaker: %w", err))
	} else {
		logInfo("FileMaker replica of the notes is on: table %s on %s, every %v", r.NotesTable, settings.FileMaker.Host, r.Interval)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		syncer.Run(ctx)
		close(done)
	}()
	return syncer, func() {
		cancel()
		<-done
	}
}

// openStore opens the storage backend chosen with STORAGE.
func openStore() (store.Store, time.Duration, error) {
	open, ok := storeOpeners[settings.Storage]
	if !ok {
		return nil, 0, fmt.Errorf("unsupported STORAGE %q", settings.Storage)
	}
	return open()
}

// Stop waits for graceful shutdown to finish: once it returns,
// kardianos/service returns from Run and the process exits.
func (p *program) Stop(s service.Service) error {
	logInfo("I'm Stopping!")
	if p.done == nil {
		return nil
	}
	p.stopOnce.Do(func() { close(p.exit) })
	<-p.done
	return p.runErr
}

// newWebAuth sets up the web app's PIN login, or returns nil when WEB_PIN is
// unset and the web app is off. Validate has checked the settings already.
func newWebAuth(w types.WebSettings) (*auth.Authenticator, error) {
	if !w.Enabled() {
		return nil, nil
	}
	return auth.New(auth.Config{
		PIN:          w.PIN,
		Secret:       []byte(w.SessionSecret),
		SessionTTL:   w.SessionTimeout,
		SecureCookie: w.CookieSecure,
		Logger:       appLogger(),
	})
}

// ensureAuthToken generates a temporary random token when AUTH_TOKEN is not
// configured, so protected routes stay locked down. The token is logged
// because it is the only way to call those routes during this run.
func ensureAuthToken() {
	if settings.AuthToken != "" {
		return
	}
	settings.AuthToken = rand.Text()
	logWarning("AUTH_TOKEN is not set; using temporary token for this run: %s", settings.AuthToken)
}

// serviceLogger adapts the kardianos/service logger, whose methods return an
// error, to logging.Logger.
type serviceLogger struct {
	l service.Logger
}

func (s serviceLogger) Infof(format string, v ...interface{})    { s.l.Infof(format, v...) }
func (s serviceLogger) Warningf(format string, v ...interface{}) { s.l.Warningf(format, v...) }
func (s serviceLogger) Errorf(format string, v ...interface{})   { s.l.Errorf(format, v...) }

// appLogger returns the service manager's logger, or the standard logger
// before one is set up (tests, early startup).
func appLogger() logging.Logger {
	if logger == nil {
		return logging.Std{}
	}
	return serviceLogger{l: logger}
}

func logInfo(format string, v ...interface{}) {
	appLogger().Infof(format, v...)
}

func logWarning(format string, v ...interface{}) {
	appLogger().Warningf(format, v...)
}

func logError(format string, v ...interface{}) {
	appLogger().Errorf(format, v...)
}
