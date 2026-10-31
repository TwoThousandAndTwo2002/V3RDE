package post

import (
	"os"
	"strings"
	"testing"
	"time"
)

// eventsWith (N1) reads the events file until it has want, at most 5 s. A job
// writes its events line after its end is visible to GET, so a test that
// reads the file at once can find it empty under load (seen in the SUP's
// parallel S3: TestRescueJobWebSearchWhenDirectoryFails, TestTouchupDoneStoresItemPhotoAndOwnerOnly).
func eventsWith(t *testing.T, path, want string) string {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for {
		b, _ := os.ReadFile(path)
		if strings.Contains(string(b), want) || time.Now().After(end) {
			return string(b)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
