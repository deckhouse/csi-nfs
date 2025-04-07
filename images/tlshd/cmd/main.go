/*
Copyright 2025 Flant JSC

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
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var (
	opt = &Opt{}
)

type Opt struct {
	TimeoutWait uint
	Mode        string
}

func (o *Opt) Parse() {
	var rootCmd = &cobra.Command{
		RunE: func(_ *cobra.Command, _ []string) error {
			if !regexp.MustCompile(`^containers$|^init-containers$`).MatchString(o.Mode) {
				return errors.New("invalid 'mode'")
			}

			if o.TimeoutWait == 0 || o.TimeoutWait > 30 {
				return errors.New("invalid 'timeout-wait'")
			}

			return nil
		},
	}

	// Exit after displaying the help information
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		cmd.Print(cmd.UsageString())
		os.Exit(0)
	})

	// Add flags
	rootCmd.Flags().StringVarP(&o.Mode, "mode", "m", "containers", "Launch mode (allowed values: 'containers' and 'init-containers').")
	rootCmd.Flags().UintVarP(&o.TimeoutWait, "timeout-wait", "t", 2, "Timeout in seconds for process execution. Applies only to the 'init-containers' launch mode (must be in the range of 1 to 30)")

	if err := rootCmd.Execute(); err != nil {
		// we expect err to be logged already
		os.Exit(1)
	}
}

func terminateProcessGracefully(cmd *exec.Cmd, done chan error) {
	log.Println("Sending SIGTERM to the process...")
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("Failed to send SIGTERM to the process: %v", err)
		return
	}

	// Wait for the process to terminate after SIGTERM
	err := <-done
	if err != nil {
		// Such a message is considered normal when a signal is sent to terminate the process.
		// "Process exited with an error after SIGTERM: signal: terminated"
		log.Printf("Process exited with an error after SIGTERM: %v", err)
	} else {
		log.Println("Process exited successfully after SIGTERM.")
	}
}

func main() {
	opt.Parse()

	log.Printf("Launch in %s mode", opt.Mode)

	path := "/opt/deckhouse/csi/bin/tlshd"
	args := []string{"-s"}
	if opt.Mode == "init-containers" {
		args = append(args, "-c", "/opt/deckhouse/csi/etc/tlshd.conf")
	} else {
		args = append(args, "-c", "/etc/tlshd.conf")

		// Environment variables for the new process
		env := os.Environ()

		// Perform the execve system call to replace the current process
		err := syscall.Exec(path, append([]string{path}, args...), env)
		if err != nil {
			log.Fatalf("Error executing exec for %s: %v", path, err)
		}
	}
	cmd := exec.Command(path, args...)

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
	// defer cancel()

	// If a timeout is specified, wrap the context with a timeout
	if opt.Mode == "init-containers" {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(opt.TimeoutWait)*time.Second)
		// defer cancel()
		log.Printf("Timeout specified: %d seconds", opt.TimeoutWait)
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
		log.Println("Context done (timeout or cancellation)")
		terminateProcessGracefully(cmd, done)
	case err := <-done: // Process finished on its own
		if err != nil {
			cancel()
			log.Fatalf("Process exited with an error: %v", err)
		}

		r := regexp.MustCompile(`\s+is\s+not\s+available`)
		if r.Match(stderrBuf.Bytes()) {
			os.Exit(1)
		}

		log.Println("Process exited successfully.")
	case sig := <-signalChan: // Received termination signal
		log.Printf("Received signal: %v", sig)
		terminateProcessGracefully(cmd, done)
	}
}
