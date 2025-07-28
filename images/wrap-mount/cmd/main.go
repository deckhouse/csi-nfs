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
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func main() {
	cmdName := filepath.Base(os.Args[0])
	realCmd := ""
	switch cmdName {
	case "mount":
		realCmd = "originalmount"
	case "umount":
		realCmd = "originalumount"
	default:
		log.Fatalf("Unknown command: %s (should be called via symlink named mount or unmount)", cmdName)
	}

	args := os.Args[1:]

	// Add "-n" if not present
	addN := true
	for _, arg := range args {
		if arg == "-n" || (len(arg) > 2 && arg[:3] == "-n,") {
			addN = false
			break
		}
	}
	if addN {
		args = append([]string{"-n"}, args...)
	}

	// Replace current process with the real mount/unmount
	if err := syscallExec(realCmd, args); err != nil {
		log.Fatalf("Failed to exec %s: %v", realCmd, err)
	}
}

func syscallExec(cmd string, args []string) error {
	binary, err := exec.LookPath(cmd)
	if err != nil {
		return err
	}
	return syscall.Exec(binary, append([]string{cmd}, args...), os.Environ())
}
