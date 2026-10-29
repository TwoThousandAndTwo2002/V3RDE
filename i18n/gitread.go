package i18n

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ReadSource reads every file under src.Dir at src.Commit from the git repo
// at repo, in one `git cat-file --batch`. The commit must be in the repo
// (the apply script fetches the ui branch first).
func ReadSource(repo string, src Source) (map[string]string, error) {
	ls := exec.Command("git", "-C", repo, "ls-tree", "-r", "--name-only", src.Commit, "--", src.Dir)
	out, err := ls.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-tree %s (fetch branch %s first): %v", src.Commit, src.Branch, err)
	}
	paths := strings.Fields(string(out))
	if len(paths) == 0 {
		return nil, fmt.Errorf("no files under %s at %s", src.Dir, src.Commit)
	}
	cmd := exec.Command("git", "-C", repo, "cat-file", "--batch")
	var in bytes.Buffer
	for _, p := range paths {
		fmt.Fprintf(&in, "%s:%s\n", src.Commit, p)
	}
	cmd.Stdin = &in
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	r := bufio.NewReader(bytes.NewReader(raw))
	files := map[string]string{}
	for _, p := range paths {
		head, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		f := strings.Fields(head)
		if len(f) != 3 || f[1] != "blob" {
			return nil, fmt.Errorf("git cat-file %s: %q", p, head)
		}
		n, _ := strconv.Atoi(f[2])
		body := make([]byte, n+1) // and the newline after the body
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, err
		}
		rel, _ := filepath.Rel(src.Dir, p)
		files[filepath.ToSlash(rel)] = string(body[:n])
	}
	return files, nil
}

// ReadJSON reads a JSON file into v; unknown fields are an error.
func ReadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

// Marshal is the one JSON form of en.json and skipped.json: 2-space indent,
// no HTML escaping, a newline at the end.
func Marshal(v any) []byte {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	if err := e.Encode(v); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// EnFile is i18n/en.json.
type EnFile struct {
	Source   Source    `json:"source"`
	Language string    `json:"language"`
	Messages []Message `json:"messages"`
}

// SkippedFile is i18n/skipped.json: every literal that is not text, with its rule.
type SkippedFile struct {
	Source  Source    `json:"source"`
	Skipped []Skipped `json:"skipped"`
}

// Build makes en.json and skipped.json from the repo and the files in dir (i18n/).
func Build(repo, dir string) (en, skipped []byte, n int, err error) {
	var src Source
	var lists Lists
	if err = ReadJSON(filepath.Join(dir, "source.json"), &src); err != nil {
		return
	}
	if err = ReadJSON(filepath.Join(dir, "rules.json"), &lists); err != nil {
		return
	}
	files, err := ReadSource(repo, src)
	if err != nil {
		return
	}
	msgs, skips, err := All(files, &lists)
	if err != nil {
		return
	}
	en = Marshal(EnFile{Source: src, Language: "en", Messages: msgs})
	skipped = Marshal(SkippedFile{Source: src, Skipped: skips})
	return en, skipped, len(msgs), nil
}
