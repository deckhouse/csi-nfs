/*
Copyright 2026 Flant JSC

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

package framework

import (
	"context"
	"errors"
	"time"
)

// PollInterval is the sampling period for every wait in this package.
const PollInterval = 5 * time.Second

var ErrPollTimeout = errors.New("timed out")

// PollUntil samples cond until done, error, or timeout. A cond error aborts
// immediately: it means the wait can never succeed.
func PollUntil(ctx context.Context, timeout time.Duration, cond func(context.Context) (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for {
		done, err := cond(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if time.Now().After(deadline) {
			return ErrPollTimeout
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(PollInterval):
		}
	}
}
