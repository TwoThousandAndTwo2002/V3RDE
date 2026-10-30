package i18n

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ReadTree reads every .ts and .tsx file under dir of the checked-out work tree
// at repo, from the disk (cmd/round: the deployed checkout, read-only; no git
// command is needed). The keys are the paths under dir, as ReadSource gives them.
func ReadTree(repo, dir string) (map[string]string, error) {
	root := filepath.Join(repo, filepath.FromSlash(dir))
	files := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !(strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".tsx")) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read %s: %v", root, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .ts or .tsx files under %s", root)
	}
	return files, nil
}

// HeadCommit reads the commit and the branch of HEAD from repo/.git with no git
// command (a .git file of a worktree, loose refs and packed-refs). The branch is
// "" for a detached HEAD.
func HeadCommit(repo string) (commit, branch string, err error) {
	gitDir := filepath.Join(repo, ".git")
	if fi, e := os.Stat(gitDir); e == nil && !fi.IsDir() {
		b, e := os.ReadFile(gitDir)
		if e != nil {
			return "", "", e
		}
		d := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "gitdir:"))
		if !filepath.IsAbs(d) {
			d = filepath.Join(repo, d)
		}
		gitDir = d
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", "", fmt.Errorf("no git HEAD in %s: %v", repo, err)
	}
	h := strings.TrimSpace(string(head))
	if !strings.HasPrefix(h, "ref: ") {
		return h, "", nil
	}
	ref := strings.TrimPrefix(h, "ref: ")
	branch = strings.TrimPrefix(ref, "refs/heads/")
	// a worktree keeps its refs in the common dir
	common := gitDir
	if b, e := os.ReadFile(filepath.Join(gitDir, "commondir")); e == nil {
		c := strings.TrimSpace(string(b))
		if !filepath.IsAbs(c) {
			c = filepath.Join(gitDir, c)
		}
		common = c
	}
	for _, d := range []string{gitDir, common} {
		if b, e := os.ReadFile(filepath.Join(d, filepath.FromSlash(ref))); e == nil {
			return strings.TrimSpace(string(b)), branch, nil
		}
	}
	f, err := os.Open(filepath.Join(common, "packed-refs"))
	if err != nil {
		return "", branch, fmt.Errorf("ref %s is not in %s", ref, repo)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if fs := strings.Fields(sc.Text()); len(fs) == 2 && fs[1] == ref {
			return fs[0], branch, nil
		}
	}
	return "", branch, errors.New("ref " + ref + " is not in " + repo)
}
