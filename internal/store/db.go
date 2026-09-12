package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var ErrNotFound = errors.New("not found")

type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Store struct {
	db dbtx
}

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Tx(ctx context.Context, fn func(*Store) error) error {
	db, ok := s.db.(*sql.DB)
	if !ok {
		return errors.New("store: already inside a transaction")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := fn(&Store{db: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

func Open(path string) (*sql.DB, error) {
	pragmas := []string{
		"_pragma=journal_mode(WAL)",
		"_pragma=busy_timeout(5000)",
		"_pragma=foreign_keys(1)",
		"_pragma=synchronous(NORMAL)",
	}
	db, err := sql.Open("sqlite", "file:"+path+"?"+strings.Join(pragmas, "&"))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping %s: %w", path, err)
	}
	return db, nil
}

func Migrate(db *sql.DB) error {
	goose.SetBaseFS(migrationFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}

// Safeguard
const MaxListRows = 500

func itoa(n int) string { return strconv.Itoa(n) }
