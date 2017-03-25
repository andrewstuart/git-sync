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
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/thockin/glogr"
	"github.com/thockin/logr"
)

var (
	log     = newLoggerOrDie()
	cliOpts = SyncOption{}
)

func main() {
	setupFlags(&cliOpts)

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

func newLoggerOrDie() logr.Logger {
	g, err := glogr.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failind to initialize logging: %v\n", err)
		os.Exit(1)
	}
	return g
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
