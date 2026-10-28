//go:build sqlite

package barcode

// OpenStore opens the catalog file.
func OpenStore(path string, want int) (Store, error) { return OpenSQLite(path, want) }
