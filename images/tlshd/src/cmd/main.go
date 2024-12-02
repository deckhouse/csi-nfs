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

	args := []string{"-s"}
	if opt.TimeoutWait > 0 {
		args = append(args, "-c", "/opt/deckhouse/csi/etc/tlshd.conf")
	} else {
		args = append(args, "-c", "/etc/tlshd.conf")
	}
	cmd := exec.Command("/opt/deckhouse/csi/bin/tlshd", args...)

	var stderrBuf bytes.Buffer
	// MultiWriter to write to both os.Stderr and the buffer
	//
	// --- because there is no non-zero return code ---
	// # ./src/tlshd/tlshd -s -c ./src/tlshd/tlshd.conf; echo $?
	// tlshd[147529]: Built from ktls-utils 0.11 on Nov 20 2024 12:52:22
	// tlshd[147529]: Kernel handshake service is not available
	// tlshd[147529]: Shutting down.
	// 0
	multiStderr := io.MultiWriter(os.Stderr, &stderrBuf)

	cmd.Stderr = multiStderr
	cmd.Stdout = os.Stdout

	err := cmd.Start()
	if err != nil {
		log.Fatalf("Failed to start the command: %v", err)
	}
	log.Printf("Process started with PID: %d", cmd.Process.Pid)

	// Create a base context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// If a timeout is specified, wrap the context with a timeout
	if opt.TimeoutWait > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(opt.TimeoutWait)*time.Second)
		defer cancel()
		log.Printf("Timeout specified: %d seconds", opt.TimeoutWait)
	} else {
		log.Println("No timeout specified. Process will run indefinitely until it finishes or receives a termination signal.")
	}

	// Channel to handle process completion
	done := make(chan error, 1)

	// Start a goroutine to monitor when the process finishes
	go func() {
		done <- cmd.Wait()
	}()

	// Channel to handle OS signals
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-ctx.Done(): // Timeout or cancellation occurred
		log.Println("Context done (timeout or cancellation), terminating the process...")
		if err := cmd.Process.Kill(); err != nil {
			log.Fatalf("Failed to terminate the process: %v", err)
		}
		log.Println("Process has been terminated.")
	case err := <-done: // Process finished on its own
		if err != nil {
			log.Fatalf("Process exited with an error: %v", err)
		}

		r := regexp.MustCompile(`\s+is\s+not\s+available`)
		if r.Match(stderrBuf.Bytes()) {
			os.Exit(1)
		}

		log.Println("Process exited successfully.")
	case sig := <-signalChan: // Received termination signal
		log.Printf("Received signal: %v, terminating the process...", sig)
		if err := cmd.Process.Kill(); err != nil {
			log.Fatalf("Failed to terminate the process: %v", err)
		}
		log.Println("Process has been terminated.")
	}
}