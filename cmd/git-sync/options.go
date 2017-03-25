package main

import (
	"fmt"
	"os"
	"path"
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
