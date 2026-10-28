//go:build !sqlite

package barcode

import "testing"

// Without the sqlite tag, OpenStore always fails, also for a file that
// exists: the service answers UNAVAILABLE (it fails closed).
func TestOpenStoreWithoutSQLiteFailsClosed(t *testing.T) {
	if _, err := OpenStore("barcode_test.go", 0); err == nil || err.Error() != "built without sqlite" {
		t.Fatalf("OpenStore: %v", err)
	}
}
