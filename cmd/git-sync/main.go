/*
Copyright 2014 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// git-sync is a command that pull a git repository to a local directory.

package main // import "k8s.io/git-sync/cmd/git-sync"

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thockin/glogr"
	"github.com/thockin/logr"
)

func newLoggerOrDie() logr.Logger {
	g, err := glogr.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failind to initialize logging: %v\n", err)
		os.Exit(1)
	}
	return g
}

func envString(key, def string) string {
	if env := os.Getenv(key); env != "" {
		return env
	}
	return def
}

func envBool(key string, def bool) bool {
	if env := os.Getenv(key); env != "" {
		res, err := strconv.ParseBool(env)
		if err != nil {
			return def
		}

		return res
	}
	return def
}

func envInt(key string, def int) int {
	if env := os.Getenv(key); env != "" {
		val, err := strconv.Atoi(env)
		if err != nil {
			log.Errorf("invalid value for %q: using default: %v", key, def)
			return def
		}
		return val
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if env := os.Getenv(key); env != "" {
		val, err := strconv.ParseFloat(env, 64)
		if err != nil {
			log.Errorf("invalid value for %q: using default: %v", key, def)
			return def
		}
		return val
	}
	return def
}

func main() {

	// From here on, output goes through logging.
	log.V(0).Infof("starting up: %q", os.Args)

	initialSync := true
	failCount := 0
	for {
		if err := cliOpts.sync(); err != nil {
			if initialSync || failCount >= cliOpts.MaxSyncFailures {
				log.Errorf("error syncing repo: %v", err)
				os.Exit(1)
			}

			failCount++
			log.Errorf("unexpected error syncing repo: %v", err)
			log.V(0).Infof("waiting %v before retrying", waitTime(cliOpts.Wait))
			time.Sleep(waitTime(cliOpts.Wait))
			continue
		}
		if initialSync {
			if isHash, err := cliOpts.revIsHash(cliOpts.Rev); err != nil {
				log.Errorf("can't tell if rev %s is a git hash, exiting", cliOpts.Rev)
				os.Exit(1)
			} else if isHash {
				log.V(0).Infof("rev %s appears to be a git hash, no further sync needed", cliOpts.Rev)
				sleepForever()
			}
			if cliOpts.OneTime {
				os.Exit(0)
			}
			initialSync = false
		}

		failCount = 0
		log.V(1).Infof("next sync in %v", waitTime(cliOpts.Wait))
		time.Sleep(waitTime(cliOpts.Wait))
	}
}

func waitTime(seconds float64) time.Duration {
	return time.Duration(int(seconds*1000)) * time.Millisecond
}

// Do no work, but don't do something that triggers go's runtime into thinking
// it is deadlocked.
func sleepForever() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)
	<-c
	os.Exit(0)
}

// updateSymlink atomically swaps the symlink to point at the specified directory and cleans up the previous worktree.
func updateSymlink(gitRoot, link, newDir string) error {
	// Get currently-linked repo directory (to be removed), unless it doesn't exist
	currentDir, err := filepath.EvalSymlinks(path.Join(gitRoot, link))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error accessing symlink: %v", err)
	}

	// newDir is /git/rev-..., we need to change it to relative path.
	// Volume in other container may not be mounted at /git, so the symlink can't point to /git.
	newDirRelative, err := filepath.Rel(gitRoot, newDir)
	if err != nil {
		return fmt.Errorf("error converting to relative path: %v", err)
	}

	if _, err := runCommand(gitRoot, "ln", "-snf", newDirRelative, "tmp-link"); err != nil {
		return fmt.Errorf("error creating symlink: %v", err)
	}
	log.V(1).Infof("created symlink %s -> %s", "tmp-link", newDirRelative)

	if _, err := runCommand(gitRoot, "mv", "-T", "tmp-link", link); err != nil {
		return fmt.Errorf("error replacing symlink: %v", err)
	}
	log.V(1).Infof("renamed symlink %s to %s", "tmp-link", link)

	// Clean up previous worktree
	if len(currentDir) > 0 {
		if err = os.RemoveAll(currentDir); err != nil {
			return fmt.Errorf("error removing directory: %v", err)
		}

		log.V(1).Infof("removed %s", currentDir)

		_, err := runCommand(gitRoot, "git", "worktree", "prune")
		if err != nil {
			return err
		}

		log.V(1).Infof("pruned old worktrees")
	}

	return nil
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

func remoteHashForRef(ref, gitRoot string) (string, error) {
	output, err := runCommand(gitRoot, "git", "ls-remote", "-q", "origin", ref)
	if err != nil {
		return "", err
	}
	parts := strings.Split(string(output), "\t")
	return parts[0], nil
}

func cmdForLog(command string, args ...string) string {
	if strings.ContainsAny(command, " \t\n") {
		command = fmt.Sprintf("%q", command)
	}
	for i := range args {
		if strings.ContainsAny(args[i], " \t\n") {
			args[i] = fmt.Sprintf("%q", args[i])
		}
	}
	return command + " " + strings.Join(args, " ")
}

func runCommand(cwd, command string, args ...string) (string, error) {
	log.V(5).Infof("run(%q): %s", cwd, cmdForLog(command, args...))

	cmd := exec.Command(command, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error running command: %v: %q", err, string(output))
	}

	return string(output), nil
}

func setupGitAuth(username, password, gitURL string) error {
	log.V(1).Infof("setting up the git credential cache")
	cmd := exec.Command("git", "config", "--global", "credential.helper", "cache")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error setting up git credentials %v: %s", err, string(output))
	}

	cmd = exec.Command("git", "credential", "approve")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	creds := fmt.Sprintf("url=%v\nusername=%v\npassword=%v\n", gitURL, username, password)
	io.Copy(stdin, bytes.NewBufferString(creds))
	stdin.Close()
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error setting up git credentials %v: %s", err, string(output))
	}

	return nil
}

func setupGitSSH() error {
	log.V(1).Infof("setting up git SSH credentials")

	var pathToSSHSecret = "/etc/git-secret/ssh"

	fileInfo, err := os.Stat(pathToSSHSecret)
	if err != nil {
		return fmt.Errorf("error: could not find SSH key Secret: %v", err)
	}

	if fileInfo.Mode() != 0400 {
		return fmt.Errorf("Permissions %s for SSH key are too open. It is recommended to mount secret volume with `defaultMode: 256` (decimal number for octal 0400).", fileInfo.Mode())
	}

	//set env variable GIT_SSH_COMMAND to force git use customized ssh command
	err = os.Setenv("GIT_SSH_COMMAND", fmt.Sprintf("ssh -q -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i %s", pathToSSHSecret))
	if err != nil {
		return fmt.Errorf("Failed to set the GIT_SSH_COMMAND env var: %v", err)
	}

	return nil
}
