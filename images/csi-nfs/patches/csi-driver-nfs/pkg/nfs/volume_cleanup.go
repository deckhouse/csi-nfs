/*
Copyright 2025 The Kubernetes Authors.

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

package nfs

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/unix"

	"k8s.io/klog/v2"
)

const (
	secureEraseMethodKey        = "secureErase"
	secureEraseMethodDisable    = "Disable"
	secureEraseMethodDiscard    = "Discard"
	secureEraseMethodSinglePass = "SinglePass"
	secureEraseMethodThreePass  = "ThreePass"
)

func getSecureEraseMethod(context map[string]string) (string, bool, error) {
	val, ok := context[secureEraseMethodKey]
	if !ok {
		return "", false, nil
	}
	if val == secureEraseMethodDisable {
		return "", false, nil
	}

	switch val {
	case secureEraseMethodDiscard, secureEraseMethodSinglePass, secureEraseMethodThreePass:
		return val, true, nil
	default:
		return "", false, fmt.Errorf("invalid secure erase method %s", val)
	}
}

func secureEraseVolume(volumePath, secureEraseMethod string) error {
	absPath, err := filepath.Abs(volumePath)
	if err != nil {
		return fmt.Errorf("getting absolute path for %s: %w", volumePath, err)
	}
	klog.V(4).Infof("Secure erasing volume %s with method %s", absPath, secureEraseMethod)

	err = filepath.Walk(absPath, func(path string, info fs.FileInfo, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walking error for %s: %w", path, walkErr)
		}

		if !info.IsDir() {
			klog.V(4).Infof("Secure erasing file %s", path)
			return secureEraseFile(path, secureEraseMethod)
		} else {
			klog.V(4).Infof("Skipping directory %s", path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error while walking through volume directory %s: %w", absPath, err)
	}

	return nil
}

func secureEraseFile(filePath, secureEraseMethod string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat file %s: %w", filePath, err)
	}

	if !info.Mode().IsRegular() {
		klog.V(4).Infof("Skipping non-regular file %s", filePath)
		return nil
	}

	switch secureEraseMethod {
	case secureEraseMethodDiscard:
		return discardFile(filePath, info)
	case secureEraseMethodSinglePass:
		return shredFile(filePath, info, 1)
	case secureEraseMethodThreePass:
		return shredFile(filePath, info, 3)
	default:
		return fmt.Errorf("invalid secure erase method %s", secureEraseMethod)
	}
}

func discardFile(filePath string, info os.FileInfo) error {
	klog.V(4).Infof("Discarding file %s", filePath)
	file, err := os.OpenFile(filePath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open file %s for discard: %w", filePath, err)
	}
	defer file.Close()

	fileSize := info.Size()
	fd := int(file.Fd())
	klog.V(4).Infof("Sending FALLOC_FL_PUNCH_HOLE|FALLOC_FL_KEEP_SIZE for file %s with size %d", filePath, fileSize)
	if err := unix.Fallocate(fd, unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE, 0, fileSize); err != nil {
		return fmt.Errorf("discard (punch hole) failed for file %s: %w", filePath, err)
	}

	klog.V(4).Infof("Discarding file %s completed.", filePath)
	// klog.V(4).Infof("Discarding file %s completed. Removing file", filePath)
	// if err := os.Remove(filePath); err != nil {
	// 	return fmt.Errorf("failed to remove file %s after discard: %w", filePath, err)
	// }

	return nil
}

func shredFile(filePath string, info os.FileInfo, passes int) error {
	klog.V(4).Infof("Shredding file %s with %d passes. Run command: shred -v -n %d %s", filePath, passes, passes, filePath)
	cmd := exec.Command("shred", "-v", "-n", fmt.Sprintf("%d", passes), filePath)

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("shred shred failed for file %s: %w, output: %s", filePath, err, string(out))
	}

	klog.V(4).Infof("Shredding file %s completed.", filePath)
	// klog.V(4).Infof("Shredding file %s completed. Removing file", filePath)
	// if err := os.Remove(filePath); err != nil {
	// 	return fmt.Errorf("failed to remove file %s after shred: %w", filePath, err)
	// }

	return nil
}
