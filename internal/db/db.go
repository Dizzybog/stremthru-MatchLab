package db

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MunifTanjim/stremthru/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/mattn/go-sqlite3"
)

const sqliteDefaultMaxConns = 8

type DB struct {
	*sql.DB
	URI     ConnectionURI
	onClose func() error

	replicas       []*sql.DB
	replicaIdx     uint32
	replicaOnClose []func() error
}

func (db *DB) getReplica() *sql.DB {
	n := len(db.replicas)
	if n == 0 {
		return db.DB
	}
	i := atomic.AddUint32(&db.replicaIdx, 1)
	return db.replicas[int(i)%n]
}

var db = &DB{}
var Dialect DBDialect

var BooleanFalse string
var BooleanTrue string
var CurrentTimestamp string
var FnJSONGroupArray string
var FnJSONObject string
var FnMax string

// maintenanceWriteGate blocks new StremThru writes and drains active
// transactions while a global MatchLab mapping reset owns the database.
// Read-only queries and the maintenance job heartbeat remain available.
var maintenanceWriteGate sync.RWMutex

var NewAdvisoryLock func(names ...string) AdvisoryLock
var InStringValues func(values []string) (query string, args []any)

var connUri, dsnModifiers = func() (ConnectionURI, []DSNModifier) {
	uri, err := ParseConnectionURI(config.DatabaseURI)
	if err != nil {
		log.Fatalf("[db] failed to parse uri: %v\n", err)
	}

	Dialect = uri.Dialect
	dsnModifiers := []DSNModifier{}

	switch Dialect {
	case DBDialectSQLite:
		BooleanFalse = "0"
		BooleanTrue = "1"
		CurrentTimestamp = "unixepoch()"
		FnJSONGroupArray = "json_group_array"
		FnJSONObject = "json_object"
		FnMax = "max"
		NewAdvisoryLock = sqliteNewAdvisoryLock
		InStringValues = sqliteInStringValues

		dsnModifiers = append(dsnModifiers, func(u *url.URL, q *url.Values) {
			u.Scheme = "file"
		})
	case DBDialectPostgres:
		BooleanFalse = "false"
		BooleanTrue = "true"
		CurrentTimestamp = "current_timestamp"
		FnJSONGroupArray = "json_agg"
		FnJSONObject = "json_build_object"
		FnMax = "greatest"
		NewAdvisoryLock = postgresNewAdvisoryLock
		InStringValues = postgresInStringValues

	default:
		log.Fatalf("[db] unsupported dialect: %v\n", Dialect)
	}

	return uri, dsnModifiers
}()

type dbExec func(query string, args ...any) (sql.Result, error)
type dbQuery func(query string, args ...any) (*sql.Rows, error)
type dbQueryRow func(query string, args ...any) *sql.Row
type Executor interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

var getExec = func(db Executor) dbExec {
	if Dialect == DBDialectPostgres {
		return func(query string, args ...any) (sql.Result, error) {
			return db.Exec(adaptQuery(query), args...)
		}
	}

	return func(query string, args ...any) (sql.Result, error) {
		retryLeft := 2
		r, err := db.Exec(query, args...)
		for err != nil && retryLeft > 0 {
			if e, ok := err.(sqlite3.Error); ok && e.Code == sqlite3.ErrBusy {
				time.Sleep(2 * time.Second)
				r, err = db.Exec(query, args...)
				retryLeft--
			} else {
				retryLeft = 0
			}
		}
		return r, err
	}
}

var execWithoutMaintenanceGate = getExec(db)

// WaitForMaintenance is a cooperative checkpoint for background workers.
// It prevents new work from starting while a reset owns the writer gate.
func WaitForMaintenance() {
	maintenanceWriteGate.RLock()
	maintenanceWriteGate.RUnlock()
}

// Exec blocks ordinary StremThru writes while the global reset is active.
// Each standalone statement holds a shared gate until it finishes.
func Exec(query string, args ...any) (sql.Result, error) {
	maintenanceWriteGate.RLock()
	defer maintenanceWriteGate.RUnlock()
	return execWithoutMaintenanceGate(query, args...)
}

// ExecMaintenanceStatus is exclusively for resetting-job log heartbeats and
// terminal status: these writes touch only job_log, never mapping tables.
func ExecMaintenanceStatus(query string, args ...any) (sql.Result, error) {
	return execWithoutMaintenanceGate(query, args...)
}

func Query(query string, args ...any) (*sql.Rows, error) {
	return db.getReplica().Query(adaptQuery(query), args...)
}

func QueryRow(query string, args ...any) *sql.Row {
	return db.getReplica().QueryRow(adaptQuery(query), args...)
}

type dbExecutor struct{}

func (e dbExecutor) Exec(query string, args ...any) (sql.Result, error) {
	return Exec(query, args...)
}

func (e dbExecutor) Query(query string, args ...any) (*sql.Rows, error) {
	return Query(query, args...)
}

func (e dbExecutor) QueryRow(query string, args ...any) *sql.Row {
	return QueryRow(query, args...)
}

var execturor = dbExecutor{}

func GetDB() *dbExecutor {
	return &execturor
}

type Tx struct {
	tx      *sql.Tx
	exec    dbExec
	onClose func()
	closed  sync.Once
}

func (tx *Tx) finish() {
	tx.closed.Do(func() {
		if tx.onClose != nil {
			tx.onClose()
		}
	})
}

func (tx *Tx) Commit() error {
	defer tx.finish()
	return tx.tx.Commit()
}

func (tx *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return tx.exec(query, args...)
}

func (tx *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return tx.tx.Query(adaptQuery(query), args...)
}

func (tx *Tx) QueryRow(query string, args ...any) *sql.Row {
	return tx.tx.QueryRow(adaptQuery(query), args...)
}

func (tx *Tx) Rollback() error {
	defer tx.finish()
	return tx.tx.Rollback()
}

func beginWithGate(onClose func()) (*Tx, error) {
	tx, err := db.Begin()
	if err != nil {
		onClose()
		return nil, err
	}
	return &Tx{tx: tx, exec: getExec(tx), onClose: onClose}, nil
}

// Begin acquires a shared maintenance gate for the full transaction lifetime.
func Begin() (*Tx, error) {
	maintenanceWriteGate.RLock()
	return beginWithGate(maintenanceWriteGate.RUnlock)
}

// BeginMaintenance waits until all in-process StremThru writers have drained,
// then blocks new writes until the reset transaction commits or rolls back.
func BeginMaintenance() (*Tx, error) {
	maintenanceWriteGate.Lock()
	return beginWithGate(maintenanceWriteGate.Unlock)
}

// BeginAdvisoryLock is only for PostgreSQL advisory lock handles. The lock
// itself is not a content writer and may outlive the entire reset; taking the
// shared maintenance gate here would deadlock BeginMaintenance.
func BeginAdvisoryLock() (*Tx, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx, exec: getExec(tx)}, nil
}

func Ping() {
	err := db.Ping()
	if err != nil {
		log.Fatalf("[db] failed to ping: %v\n", err)
	}
	one := 0
	row := db.QueryRow(adaptQuery("SELECT 1"))
	if err := row.Scan(&one); err != nil {
		log.Fatalf("[db] failed to query: %v\n", err)
	}
	for i, replica := range db.replicas {
		if err := replica.Ping(); err != nil {
			log.Fatalf("[db] failed to ping replica[%d]: %v\n", i, err)
		}
		row := replica.QueryRow(adaptQuery("SELECT 1"))
		if err := row.Scan(&one); err != nil {
			log.Fatalf("[db] failed to query replica[%d]: %v\n", i, err)
		}
	}
}

func Open() *DB {
	switch connUri.Dialect {
	case DBDialectSQLite:
		database, err := sql.Open(connUri.DriverName, connUri.DSN(dsnModifiers...))
		if err != nil {
			log.Fatalf("[db] failed to open: %v\n", err)
		}
		// Each connection holds its own page cache (see _cache_size), so bound
		// the pool (configurable via max_conns) to keep RSS in check.
		maxConns := connUri.MaxConnection
		if maxConns == 0 {
			maxConns = sqliteDefaultMaxConns
		}
		database.SetMaxOpenConns(maxConns)
		if connUri.MinConnection != 0 {
			database.SetMaxIdleConns(connUri.MinConnection)
		}
		db.DB = database
		db.onClose = func() error {
			err := db.DB.Close()
			return err
		}

	case DBDialectPostgres:
		pool, err := pgxpool.New(context.Background(), connUri.DSN())
		if err != nil {
			log.Fatalf("[db] failed to create connection pool: %v\n", err)
		}
		db.DB = stdlib.OpenDBFromPool(pool)
		db.onClose = func() error {
			err := db.DB.Close()
			pool.Close()
			return err
		}
	}

	db.URI = connUri

	for _, replicaUri := range config.DatabaseReplicaURIs {
		if connUri.Dialect != DBDialectPostgres {
			log.Fatalf("[db] replica not supported for %s\n", connUri.Dialect)
		}

		replicaUri, err := ParseConnectionURI(replicaUri)
		if err != nil {
			log.Fatalf("[db] failed to parse replica uri: %v\n", err)
		}
		if replicaUri.Dialect != connUri.Dialect {
			log.Fatalf("[db] replica dialect mismatch: %s\n", replicaUri.Dialect)
		}
		pool, err := pgxpool.New(context.Background(), replicaUri.DSN())
		if err != nil {
			log.Fatalf("[db] failed to create replica connection pool: %v\n", err)
		}
		replica := stdlib.OpenDBFromPool(pool)
		db.replicas = append(db.replicas, replica)
		db.replicaOnClose = append(db.replicaOnClose, func() error {
			err := replica.Close()
			pool.Close()
			return err
		})
	}

	return db
}

func Close() error {
	errs := []error{}
	if db.onClose != nil {
		errs = append(errs, db.onClose())
	}
	for _, onClose := range db.replicaOnClose {
		if onClose != nil {
			errs = append(errs, onClose())
		}
	}
	return errors.Join(errs...)
}
