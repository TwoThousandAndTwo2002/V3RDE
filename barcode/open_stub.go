//go:build !sqlite

package barcode

import "errors"

// OpenStore without the sqlite tag always fails, so the service answers
// UNAVAILABLE (it fails closed). The plain `go test` runs this way.
func OpenStore(path string, want int) (Store, error) {
	return nil, errors.New("built without sqlite")
}
