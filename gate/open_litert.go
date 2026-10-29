//go:build litert

package gate

// OpenModel loads the gate model on CPU with the given thread count.
func OpenModel(path string, threads int) (Embedder, error) { return OpenLiteRT(path, threads) }
