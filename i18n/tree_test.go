package i18n

import (
	"os"
	"path/filepath"
	"testing"
)

func put(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ReadTree reads .ts and .tsx files of the work tree (not node_modules); HeadCommit
// reads a loose ref, packed-refs, a detached HEAD and a worktree's .git file.
func TestTreeAndHead(t *testing.T) {
	r := t.TempDir()
	put(t, filepath.Join(r, "mobile/src/a.ts"), "x")
	put(t, filepath.Join(r, "mobile/src/s/b.tsx"), "y")
	put(t, filepath.Join(r, "mobile/src/c.png"), "z")
	put(t, filepath.Join(r, "mobile/src/node_modules/d.ts"), "w")
	f, err := ReadTree(r, "mobile/src")
	if err != nil || len(f) != 2 || f["a.ts"] != "x" || f["s/b.tsx"] != "y" {
		t.Fatalf("tree %v %v", f, err)
	}
	put(t, filepath.Join(r, ".git/HEAD"), "ref: refs/heads/services\n")
	put(t, filepath.Join(r, ".git/packed-refs"), "# pack-refs\nabc123 refs/heads/services\n")
	if c, b, err := HeadCommit(r); c != "abc123" || b != "services" || err != nil {
		t.Fatalf("packed: %s %s %v", c, b, err)
	}
	put(t, filepath.Join(r, ".git/refs/heads/services"), "def456\n")
	if c, _, _ := HeadCommit(r); c != "def456" {
		t.Fatalf("loose: %s", c)
	}
	w := t.TempDir()
	put(t, filepath.Join(w, ".git"), "gitdir: "+filepath.Join(r, ".git/worktrees/w")+"\n")
	put(t, filepath.Join(r, ".git/worktrees/w/HEAD"), "0123abcd\n")
	if c, b, err := HeadCommit(w); c != "0123abcd" || b != "" || err != nil {
		t.Fatalf("worktree, detached: %s %s %v", c, b, err)
	}
}
