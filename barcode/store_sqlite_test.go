//go:build sqlite

package barcode

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// catalogFile writes a small catalog in the catalog's schema.
func catalogFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "products.sqlite")
	db, err := sql.Open("sqlite3", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		"CREATE TABLE p (gtin14 TEXT PRIMARY KEY, title TEXT NOT NULL, servings REAL NOT NULL, weight_kg REAL NOT NULL) WITHOUT ROWID",
		"INSERT INTO p VALUES ('04006381333931','PRETZEL STICKS',10.0,0.25)",
		"INSERT INTO p VALUES ('00036000291452','CHEESE CRACKERS',6.0,0.198)",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestSQLiteCatalog(t *testing.T) {
	p := catalogFile(t)
	if _, err := OpenSQLite(p, 3); err == nil || err.Error() != "catalog has 2 rows, want 3" {
		t.Fatalf("a catalog with the wrong row count: %v", err)
	}
	if _, err := OpenSQLite(filepath.Join(t.TempDir(), "none.sqlite"), 0); err == nil {
		t.Fatal("a missing file must not open")
	}
	s, err := OpenSQLite(p, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Rows() != 2 {
		t.Fatal(s.Rows())
	}
	prod, _, found, _, err := Lookup(s, "036000291452")
	if err != nil || !found || prod.Title != "CHEESE CRACKERS" || prod.Weight != 0.198 || prod.NumberOfServing != 6 {
		t.Fatalf("%+v %v", prod, err)
	}
	if _, _, found, _, err := Lookup(s, "96385074"); found || err != nil {
		t.Fatal("a miss")
	}
	var keys []string
	if err := s.Each(func(g string, _ Row) error { keys = append(keys, g); return nil }); err != nil ||
		len(keys) != 2 || keys[0] != "00036000291452" || keys[1] != "04006381333931" {
		t.Fatalf("each %v %v", keys, err)
	}
	if _, err := s.db.Exec("DELETE FROM p"); err == nil {
		t.Fatal("the catalog is writable")
	}
	st, err := OpenStore(p, 2)
	if err != nil || st.Rows() != 2 {
		t.Fatalf("OpenStore %v", err)
	}
	st.(*SQLite).Close()
}

func TestSQLiteRefusals(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.sqlite")
	db, err := sql.Open("sqlite3", empty)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE other (x)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := OpenSQLite(empty, 0); err == nil || !strings.HasPrefix(err.Error(), "count: ") {
		t.Fatalf("a file without table p: %v", err)
	}
	// a row with a NULL title is a store error, not a hit
	bad := filepath.Join(dir, "null.sqlite")
	db, _ = sql.Open("sqlite3", bad)
	db.Exec("CREATE TABLE p (gtin14 TEXT PRIMARY KEY, title TEXT, servings REAL, weight_kg REAL) WITHOUT ROWID")
	db.Exec("INSERT INTO p VALUES ('00036000291452', NULL, 1, 0.1)")
	db.Close()
	s, err := OpenSQLite(bad, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, _, found, _, err := Lookup(s, "036000291452"); found || err == nil {
		t.Fatalf("a NULL row: found %v err %v", found, err)
	}
}
