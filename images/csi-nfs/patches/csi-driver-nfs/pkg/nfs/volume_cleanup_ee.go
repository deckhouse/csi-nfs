//go:build !ce

/*
Copyright 2025 Flant JSC
Licensed under the Deckhouse Platform Enterprise Edition (EE) license.
See https://github.com/deckhouse/deckhouse/blob/main/ee/LICENSE
*/

package nfs

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	commonfeature "github.com/deckhouse/csi-nfs/lib/go/common/pkg/feature"
	"golang.org/x/sys/unix"

	"k8s.io/klog/v2"
)

func cleanupVolume(volumePath, volumeCleanupMethod string) error {
	if !commonfeature.VolumeCleanupEnabled() {
		klog.Error("Volume cleanup enabled with method %s, but volume cleanup is not supported in your edition", volumeCleanupMethod)
		return nil
	}

	klog.V(2).Infof("volume cleanup enabled, using method %v. Cleanup subdirectory at %v", volumeCleanupMethod, volumePath)
	absPath, err := filepath.Abs(volumePath)
	if err != nil {
		return fmt.Errorf("getting absolute path for %s: %w", volumePath, err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		klog.Warning("Volume directory %s does not exist, skipping cleanup", absPath)
		return nil
	}

	err = filepath.Walk(absPath, func(path string, info fs.FileInfo, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walking error for %s: %w", path, walkErr)
		}

		if !info.IsDir() {
			klog.V(4).Infof("Cleanup file %s", path)
			return cleanupFile(path, volumeCleanupMethod)
		} else {
			klog.V(4).Infof("Skipping directory %s", path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error while walking through volume directory %s: %w", absPath, err)
	}

	klog.V(2).Infof("Volume cleanup completed for %s", volumePath)
	return nil
}

func cleanupFile(filePath, volumeCleanupMethod string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat file %s: %w", filePath, err)
	}

	if !info.Mode().IsRegular() {
		klog.V(4).Infof("Skipping non-regular file %s", filePath)
		return nil
	}

	switch volumeCleanupMethod {
	case volumeCleanupMethodDiscard:
		return discardFile(filePath, info)
	case volumeCleanupMethodSinglePass:
		return shredFile(filePath, info, 1)
	case volumeCleanupMethodThreePass:
		return shredFile(filePath, info, 3)
	default:
		return fmt.Errorf("invalid volume cleanup method %s", volumeCleanupMethod)
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
