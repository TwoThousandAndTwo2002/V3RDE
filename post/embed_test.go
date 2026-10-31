package post

// X49f: the post image builds the listings package (voice uses its limits),
// so every file that listings embeds (//go:embed) must be in the image's
// build context and copied by post/Dockerfile. X49e added listings/zcta.csv
// and the box build of post failed ("pattern zcta.csv: no matching files").

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestListingsEmbedsAreInThePostImage(t *testing.T) {
	docker, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	ignore, err := os.ReadFile("Dockerfile.dockerignore")
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob("../listings/*.go")
	embed := regexp.MustCompile(`(?m)^//go:embed\s+(.+)$`)
	found := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, m := range embed.FindAllStringSubmatch(string(b), -1) {
			for _, name := range strings.Fields(m[1]) {
				found++
				if !strings.Contains(string(docker), "COPY listings/"+name+" ./listings/") {
					t.Errorf("post/Dockerfile does not copy listings/%s (embedded by %s)", name, f)
				}
				if !strings.Contains(string(ignore), "!listings/"+name+"\n") {
					t.Errorf("post/Dockerfile.dockerignore does not let in listings/%s", name)
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("no //go:embed in ../listings: the check reads the wrong folder")
	}
}
