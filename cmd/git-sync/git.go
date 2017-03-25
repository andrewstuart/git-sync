package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

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

func remoteHashForRef(ref, gitRoot string) (string, error) {
	output, err := runCommand(gitRoot, "git", "ls-remote", "-q", "origin", ref)
	if err != nil {
		return "", err
	}
	parts := strings.Split(string(output), "\t")
	return parts[0], nil
}
