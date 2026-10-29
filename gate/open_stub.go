//go:build !litert

package gate

import "errors"

// OpenModel without the litert tag always fails, so the service answers
// UNAVAILABLE (it fails closed). The plain go test runs this way.
func OpenModel(path string, threads int) (Embedder, error) {
	return nil, errors.New("built without litert")
}
