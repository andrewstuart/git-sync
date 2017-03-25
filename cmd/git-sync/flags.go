package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func setupFlags(cliOpts *SyncOption) {
	flag.StringVar(&cliOpts.Repo, "repo", envString("GIT_SYNC_REPO", ""),
		"the git repository to clone")
	flag.StringVar(&cliOpts.Branch, "branch", envString("GIT_SYNC_BRANCH", "master"),
		"the git branch to check out")
	flag.StringVar(&cliOpts.Rev, "rev", envString("GIT_SYNC_REV", "HEAD"),
		"the git revision (tag or hash) to check out")
	flag.IntVar(&cliOpts.Depth, "depth", envInt("GIT_SYNC_DEPTH", 0),
		"use a shallow clone with a history truncated to the specified number of commits")

	flag.StringVar(&cliOpts.Root, "root", envString("GIT_SYNC_ROOT", "/git"),
		"the root directory for git operations")
	flag.StringVar(&cliOpts.Dest, "dest", envString("GIT_SYNC_DEST", ""),
		"the name at which to publish the checked-out files under --root (&defaults to leaf dir of --root)")
	flag.Float64Var(&cliOpts.Wait, "wait", envFloat("GIT_SYNC_WAIT", 0),
		"the number of seconds between syncs")
	flag.BoolVar(&cliOpts.OneTime, "one-time", envBool("GIT_SYNC_ONE_TIME", false),
		"exit after the initial checkout")
	flag.IntVar(&cliOpts.MaxSyncFailures, "max-sync-failures", envInt("GIT_SYNC_MAX_SYNC_FAILURES", 0),
		"the number of consecutive failures allowed before aborting (&the first pull must succeed)")
	flag.IntVar(&cliOpts.Chmod, "change-permissions", envInt("GIT_SYNC_PERMISSIONS", 0),
		"the file permissions to apply to the checked-out files")

	flag.StringVar(&cliOpts.Username, "username", envString("GIT_SYNC_USERNAME", ""),
		"the username to use")
	flag.StringVar(&cliOpts.Password, "password", envString("GIT_SYNC_PASSWORD", ""),
		"the password to use")

	flag.BoolVar(&cliOpts.SSH, "ssh", envBool("GIT_SYNC_SSH", false),
		"use SSH for git operations")

	setFlagDefaults()

	flag.Parse()
	if cliOpts.Repo == "" {
		fmt.Fprintf(os.Stderr, "ERROR: --repo or $GIT_SYNC_REPO must be provided\n")
		flag.Usage()
		os.Exit(1)
	}
	if cliOpts.Dest == "" {
		parts := strings.Split(strings.Trim(cliOpts.Repo, "/"), "/")
		cliOpts.Dest = parts[len(parts)-1]
	}
	if strings.Contains(cliOpts.Dest, "/") {
		fmt.Fprintf(os.Stderr, "ERROR: --dest must be a bare name\n")
		flag.Usage()
		os.Exit(1)
	}
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: git executable not found: %v\n", err)
		os.Exit(1)
	}

	if cliOpts.Username != "" && cliOpts.Password != "" {
		if err := setupGitAuth(cliOpts.Username, cliOpts.Password, cliOpts.Repo); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: can't create .netrc file: %v\n", err)
			os.Exit(1)
		}
	}

	if cliOpts.SSH {
		if err := setupGitSSH(); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: can't configure SSH: %v\n", err)
			os.Exit(1)
		}
	}
}

func setFlagDefaults() {
	// Force logging to stderr.
	stderrFlag := flag.Lookup("logtostderr")
	if stderrFlag == nil {
		fmt.Fprintf(os.Stderr, "can't find flag 'logtostderr'\n")
		os.Exit(1)
	}
	stderrFlag.Value.Set("true")
}
