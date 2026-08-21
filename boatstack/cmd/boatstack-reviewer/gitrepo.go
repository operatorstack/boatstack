package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// receiptDirectory is the tracked directory that carries sealed review
// receipts. It is excluded from the reviewed-tree binding so that committing
// a sealed receipt does not invalidate the tree it binds.
const receiptDirectory = ".github/reviews"

type gitRepo struct {
	Root   string
	GitDir string
}

func openRepo(path string) (*gitRepo, error) {
	root, err := gitOutput(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not inside a Git repository: %w", path, err)
	}
	gitDir, err := gitOutput(path, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	return &gitRepo{Root: strings.TrimSpace(root), GitDir: strings.TrimSpace(gitDir)}, nil
}

func gitOutput(dir string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = dir
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (r *gitRepo) output(args ...string) (string, error) {
	return gitOutput(r.Root, args...)
}

func (r *gitRepo) revParse(revision string) (string, error) {
	value, err := r.output("rev-parse", "--verify", revision+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (r *gitRepo) headCommit() (string, error) {
	return r.revParse("HEAD")
}

func (r *gitRepo) mergeBase(baseRef, head string) (string, error) {
	value, err := r.output("merge-base", baseRef, head)
	if err != nil {
		return "", fmt.Errorf("merge base of %s and %s is unavailable: %w", baseRef, head, err)
	}
	return strings.TrimSpace(value), nil
}

// reviewedTree computes the receipt-excluded tree identity of one commit:
// the git tree hash with the sealed-receipt directory removed. Two commits
// that differ only in sealed receipts share one reviewed tree, which is what
// lets a converged receipt be committed without invalidating itself.
func (r *gitRepo) reviewedTree(commit string) (string, error) {
	indexFile, err := os.CreateTemp("", "boatstack-review-index-")
	if err != nil {
		return "", err
	}
	indexPath := indexFile.Name()
	indexFile.Close()
	os.Remove(indexPath)
	defer os.Remove(indexPath)
	environment := append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	run := func(args ...string) (string, error) {
		command := exec.Command("git", args...)
		command.Dir = r.Root
		command.Env = environment
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		if err := command.Run(); err != nil {
			return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
		}
		return stdout.String(), nil
	}
	if _, err := run("read-tree", commit+"^{tree}"); err != nil {
		return "", err
	}
	if _, err := run("rm", "--cached", "-r", "-q", "--ignore-unmatch", "--", receiptDirectory); err != nil {
		return "", err
	}
	tree, err := run("write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(tree), nil
}

// worktreeStatus reports whether the worktree differs from HEAD. Paths under
// the sealed-receipt directory and untracked files outside it do not affect
// the review decision but tracked modifications do: a review can only bind a
// tree that a commit can reproduce.
func (r *gitRepo) worktreeStatus() (dirty bool, fingerprint string, err error) {
	porcelain, err := r.output("status", "--porcelain")
	if err != nil {
		return false, "", err
	}
	var relevant []string
	for _, line := range strings.Split(porcelain, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		entry := line
		if len(entry) >= 3 {
			path := strings.TrimSpace(entry[3:])
			if renamed := strings.SplitN(path, " -> ", 2); len(renamed) == 2 {
				path = renamed[1]
			}
			path = strings.Trim(path, "\"")
			if strings.HasPrefix(path, receiptDirectory+"/") {
				continue
			}
			if strings.HasPrefix(entry, "??") {
				continue
			}
		}
		relevant = append(relevant, entry)
	}
	if len(relevant) == 0 {
		return false, "", nil
	}
	return true, sha256Hex([]byte(strings.Join(relevant, "\n"))), nil
}

// pullRequestDiff reproduces the exact diff basis the retired CI reviewer
// used: merge-base to head, zero context, no renames, no external drivers.
func (r *gitRepo) pullRequestDiff(mergeBase, head string) (string, error) {
	return r.output(
		"-c", "core.quotePath=false",
		"diff", "--no-ext-diff", "--no-renames", "--unified=0",
		mergeBase, head,
	)
}

func (r *gitRepo) showFile(revision, path string) ([]byte, error) {
	value, err := r.output("show", revision+":"+path)
	if err != nil {
		return nil, err
	}
	return []byte(value), nil
}

func (r *gitRepo) receiptDirectoryPath() string {
	return filepath.Join(r.Root, filepath.FromSlash(receiptDirectory))
}
