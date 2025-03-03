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

package nfs

import (
	"fmt"
)

const (
	volumeCleanupMethodKey        = "volumeCleanup"
	volumeCleanupMethodDiscard    = "Discard"
	volumeCleanupMethodSinglePass = "RandomFillSinglePass"
	volumeCleanupMethodThreePass  = "RandomFillThreePass"
)

func getVolumeCleanupMethod(secretData map[string]string) (string, bool, error) {
	val, ok := secretData[volumeCleanupMethodKey]
	if !ok {
		return "", false, nil
	}

	switch val {
	case volumeCleanupMethodDiscard, volumeCleanupMethodSinglePass, volumeCleanupMethodThreePass:
		return val, true, nil
	default:
		return "", false, fmt.Errorf("invalid volume cleanup method %s", val)
	}
}
