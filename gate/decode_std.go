//go:build !turbojpeg

package gate

// Without the turbojpeg tag (the plain go test) every format goes through
// Go's image package; the service image is built with -tags "litert turbojpeg".

// JPEGDecoder names the JPEG path in /healthz.
const JPEGDecoder = "go image/jpeg"

func isJPEG([]byte) bool { return false }

func decodeJPEG([]byte) ([]uint8, int, int, error) { panic("unreachable: isJPEG is false") }
