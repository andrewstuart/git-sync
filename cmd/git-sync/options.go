package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// SyncOption contains the options available for gitSync to sync
type SyncOption struct {
	Username string `json:"username"`
	Password string `json:"password"`
	SSH      bool   `json:"useSSH"`

	Repo            string  `json:"repo"`
	Branch          string  `json:"branch"`
	Rev             string  `json:"rev"`
	Depth           int     `json:"depth"`
	Root            string  `json:"root"`
	Dest            string  `json:"dest"`
	Wait            float64 `json:"wait"`
	OneTime         bool    `json:"oneTime"`
	MaxSyncFailures int     `json:"maxSyncFailures"`
	Chmod           int     `json:"chmod"`
}

func (o *SyncOption) sync() error {
	// syncRepo syncs the branch of a given repository to the destination at the given rev.
	target := path.Join(o.Repo, o.Dest)
	gitRepoPath := path.Join(target, ".git")
	hash := o.Rev
	_, err := os.Stat(gitRepoPath)
	switch {
	case os.IsNotExist(err):
		err = o.cloneRepo()
		if err != nil {
			return err
		}
		hash, err = o.hashForRev(o.Rev)
		if err != nil {
			return err
		}
	case err != nil:
		return fmt.Errorf("error checking if repo exists %q: %v", gitRepoPath, err)
	default:
		local, remote, err := o.getRevs(o.Rev)
		if err != nil {
			return err
		}
		log.V(2).Infof("local hash:  %s", local)
		log.V(2).Infof("remote hash: %s", remote)
		if local != remote {
			log.V(0).Infof("update required")
			hash = remote
		} else {
			log.V(1).Infof("no update required")
			return nil
		}
	}

	return o.addWorktreeAndSwap(hash)
}

func (o *SyncOption) cloneRepo() error {
	args := []string{"clone", "--no-checkout", "-b", o.Branch}
	if o.Depth != 0 {
		args = append(args, "--depth", strconv.Itoa(o.Depth))
	}
	args = append(args, o.Repo, o.Root)
	_, err := runCommand("", "git", args...)
	if err != nil {
		return err
	}
	log.V(0).Infof("cloned %s", o.Repo)

	return nil
}

func (o *SyncOption) hashForRev(rev string) (string, error) {
	output, err := runCommand(o.Root, "git", "rev-list", "-n1", rev)
	if err != nil {
		return "", err
	}
	return strings.Trim(string(output), "\n"), nil
}

func (o *SyncOption) revIsHash(rev string) (bool, error) {
	// If a rev is a tag name or HEAD, rev-list will produce the git hash.  If
	// it is already a git hash, the output will be the same hash.  Of course, a
	// user could specify "abc" and match "abcdef12345678", so we just do a
	// prefix match.
	output, err := o.hashForRev(rev)
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(output, rev), nil
}

// getRevs returns the local and upstream hashes for rev.
func (o *SyncOption) getRevs(rev string) (string, string, error) {
	// Ask git what the exact hash is for rev.
	local, err := o.hashForRev(rev)
	if err != nil {
		return "", "", err
	}

	// Build a ref string, depending on whether the user asked to track HEAD or a tag.
	ref := ""
	if o.Rev == "HEAD" {
		ref = "refs/heads/" + o.Branch
	} else {
		ref = "refs/tags/" + o.Rev + "^{}"
	}

	// Figure out what hash the remote resolves ref to.
	remote, err := remoteHashForRef(ref, o.Root)
	if err != nil {
		return "", "", err
	}

	return local, remote, nil
}

// addWorktreeAndSwap creates a new worktree and calls updateSymlink to swap the symlink to point to the new worktree
func (o *SyncOption) addWorktreeAndSwap(hash string) error {
	log.V(0).Infof("syncing to %s (%s)", o.Rev, hash)

	// Update from the remote.
	if _, err := runCommand(o.Root, "git", "fetch", "--tags", "origin", o.Branch); err != nil {
		return err
	}

	// Make a worktree for this exact git hash.
	worktreePath := path.Join(o.Root, "rev-"+hash)
	_, err := runCommand(o.Root, "git", "worktree", "add", worktreePath, "origin/"+o.Branch)
	if err != nil {
		return err
	}
	log.V(0).Infof("added worktree %s for origin/%s", worktreePath, o.Branch)

	// The .git file in the worktree directory holds a reference to
	// /git/.git/worktrees/<worktree-dir-name>. Replace it with a reference
	// using relative paths, so that other containers can use a different volume
	// mount name.
	worktreePathRelative, err := filepath.Rel(o.Root, worktreePath)
	if err != nil {
		return err
	}
	gitDirRef := []byte(path.Join("gitdir: ../.git/worktrees", worktreePathRelative) + "\n")
	if err = ioutil.WriteFile(path.Join(worktreePath, ".git"), gitDirRef, 0644); err != nil {
		return err
	}

	// Reset the worktree's working copy to the specific rev.
	_, err = runCommand(worktreePath, "git", "reset", "--hard", hash)
	if err != nil {
		return err
	}
	log.V(0).Infof("reset worktree %s to %s", worktreePath, hash)

	if o.Chmod != 0 {
		// set file permissions
		_, err = runCommand("", "chmod", "-R", strconv.Itoa(o.Chmod), worktreePath)
		if err != nil {
			return err
		}
	}

	return updateSymlink(o.Root, o.Dest, worktreePath)
}
