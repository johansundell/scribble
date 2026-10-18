package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/johansundell/scribble/types"
)

// mysqlConfig maps the MYSQL_* settings to a driver config that parses
// DATETIME values as UTC.
func mysqlConfig(s types.MySQLSettings) mysql.Config {
	cfg := mysql.NewConfig()
	cfg.User = s.Username
	cfg.Passwd = s.Password
	if len(s.Host) > 0 && s.Host[0] == '/' {
		cfg.Net = "unix"
		cfg.Addr = s.Host
	} else {
		cfg.Net = "tcp"
		cfg.Addr = s.Host + ":" + s.Port
	}
	cfg.DBName = s.Database
	cfg.AllowNativePasswords = true
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	return *cfg
}

// openMySQL connects to MySQL and configures the connection pool.
func openMySQL(s types.MySQLSettings) (*sql.DB, error) {
	cfg := mysqlConfig(s)
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}

	// Connection pool tuning
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	return db, nil
}

// NewMySQL connects to MySQL and returns a store that owns the connection.
// Timestamps are stored in UTC.
func NewMySQL(s types.MySQLSettings) (Store, error) {
	db, err := openMySQL(s)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS request_logs (
		id INT AUTO_INCREMENT PRIMARY KEY,
		status INT,
		method TEXT,
		error TEXT,
		endpoint TEXT,
		created_at DATETIME,
		response MEDIUMTEXT,
		request MEDIUMTEXT
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}

	// Update existing schema; modifying a column that is already MEDIUMTEXT is cheap and idempotent.
	_, err = db.Exec(`ALTER TABLE request_logs MODIFY request MEDIUMTEXT, MODIFY response MEDIUMTEXT`)
	if err != nil {
		db.Close()
		return nil, err
	}

	return &mysqlStore{
		SQLStore: &SQLStore{
			db:      db,
			timeArg: func(t time.Time) any { return t.UTC() },
			noLimit: 1<<63 - 1, // MySQL has no "no limit" value; use the documented maximum
		},
	}, nil
}

type mysqlStore struct {
	*SQLStore
}

func (s *mysqlStore) LogRequests(ctx context.Context, entries []types.UsageLog) error {
	err := s.SQLStore.LogRequests(ctx, entries)
	if err != nil {
		var myErr *mysql.MySQLError
		if errors.As(err, &myErr) && myErr.Number == 1406 {
			return Permanent(err)
		}
	}
	return err
}
