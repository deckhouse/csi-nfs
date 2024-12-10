/*
Copyright 2024 Flant JSC

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

package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"syscall"
	"time"
)

var (
	opt = &Opt{}
)

type Opt struct {
	TimeoutWait uint
}

func (o *Opt) Parse() {
	flag.UintVar(&o.TimeoutWait, "timeout_wait", 0, "timeout in seconds for process execution (0 means no timeout)")
	flag.Parse()
}

func main() {
	opt.Parse()

	// Create a base context
	ctx, _ := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	args := []string{"-s"}
	if opt.TimeoutWait > 0 {
		args = append(args, "-c", "/opt/deckhouse/csi/etc/tlshd.conf")

		tctx, cancel := context.WithTimeout(ctx, time.Duration(opt.TimeoutWait)*time.Second)
		defer cancel()
		ctx = tctx

		log.Printf("Timeout specified: %d seconds", opt.TimeoutWait)

	} else {
		args = append(args, "-c", "/etc/tlshd.conf")
		log.Println("No timeout specified. Process will run indefinitely until it finishes or receives a termination signal.")
	}

	cmd := exec.CommandContext(ctx, "/opt/deckhouse/csi/bin/tlshd", args...)

	var stderrBuf bytes.Buffer
	// MultiWriter to write to both os.Stderr and the buffer
	// --- because there is no non-zero return code ---
	// # ./src/tlshd/tlshd -s -c ./src/tlshd/tlshd.conf; echo $?
	// tlshd[147529]: Built from ktls-utils 0.11 on Nov 20 2024 12:52:22
	// tlshd[147529]: Kernel handshake service is not available
	// tlshd[147529]: Shutting down.
	// 0
	multiStderr := io.MultiWriter(os.Stderr, &stderrBuf)

	cmd.Stderr = multiStderr
	cmd.Stdout = os.Stdout
	cmd.WaitDelay = time.Second * 2

	// run
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to run process: %v", err)
	}

	r := regexp.MustCompile(`\s+is\s+not\s+available`)
	if r.Match(stderrBuf.Bytes()) {
		os.Exit(1)
	}
	log.Printf("success")
}
