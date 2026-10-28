//go:build sqlite

package barcode

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	_ "github.com/mattn/go-sqlite3" // SQLite (cgo)
)

// SQLite is the catalog file, opened read-only and immutable (it is a
// pinned, read-only mount), with one prepared query.
type SQLite struct {
	db   *sql.DB
	get  *sql.Stmt
	rows int
}

// OpenSQLite opens path and refuses a file whose row count is not want
// (when want > 0): see ExpectedRows. mode=ro and immutable=1 are SQLite URI
// parameters (https://sqlite.org/uri.html, "Recognized Query Parameters");
// _query_only is the driver's query_only pragma.
func OpenSQLite(path string, want int) (*SQLite, error) {
	db, err := sql.Open("sqlite3", "file:"+url.PathEscape(path)+"?mode=ro&immutable=1&_query_only=1")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	fail := func(err error) (*SQLite, error) {
		db.Close()
		return nil, err
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM p").Scan(&n); err != nil {
		return fail(fmt.Errorf("count: %w", err))
	}
	if want > 0 && n != want {
		return fail(fmt.Errorf("catalog has %d rows, want %d", n, want))
	}
	st, err := db.Prepare("SELECT title, servings, weight_kg FROM p WHERE gtin14 = ? LIMIT 1")
	if err != nil {
		return fail(err)
	}
	return &SQLite{db: db, get: st, rows: n}, nil
}

// Get is the row of g14.
func (s *SQLite) Get(g14 string) (Row, bool, error) {
	var r Row
	switch err := s.get.QueryRow(g14).Scan(&r.Title, &r.Servings, &r.WeightKg); {
	case errors.Is(err, sql.ErrNoRows):
		return Row{}, false, nil
	case err != nil:
		return Row{}, false, err
	}
	return r, true, nil
}

// Rows is the row count found at open.
func (s *SQLite) Rows() int { return s.rows }

// Each calls f for every row in key order (a full scan, for checks).
func (s *SQLite) Each(f func(g14 string, r Row) error) error {
	q, err := s.db.Query("SELECT gtin14, title, servings, weight_kg FROM p ORDER BY gtin14")
	if err != nil {
		return err
	}
	defer q.Close()
	for q.Next() {
		var g string
		var r Row
		if err := q.Scan(&g, &r.Title, &r.Servings, &r.WeightKg); err != nil {
			return err
		}
		if err := f(g, r); err != nil {
			return err
		}
	}
	return q.Err()
}

// Close closes the file.
func (s *SQLite) Close() error {
	s.get.Close()
	return s.db.Close()
}
