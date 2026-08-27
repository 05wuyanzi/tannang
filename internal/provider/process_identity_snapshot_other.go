// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build !windows

package provider

import (
	"errors"

	"github.com/05wuyanzi/tannang/internal/execution"
)

var errProcessSnapshotUnsupportedOS = errors.New("Tool Help process snapshots require Windows")

type unsupportedProcessSnapshotAPI struct{}

func newPlatformProcessSnapshotAPI() processSnapshotAPI {
	return unsupportedProcessSnapshotAPI{}
}

func (unsupportedProcessSnapshotAPI) availability() (execution.Reason, error) {
	return execution.ReasonUnsupportedOS, errProcessSnapshotUnsupportedOS
}

func (unsupportedProcessSnapshotAPI) create() (uintptr, error) {
	return 0, errProcessSnapshotUnsupportedOS
}

func (unsupportedProcessSnapshotAPI) first(uintptr, *processEntry32W) (bool, error) {
	return false, errProcessSnapshotUnsupportedOS
}

func (unsupportedProcessSnapshotAPI) next(uintptr, *processEntry32W) (bool, error) {
	return false, errProcessSnapshotUnsupportedOS
}

func (unsupportedProcessSnapshotAPI) close(uintptr) error {
	return errProcessSnapshotUnsupportedOS
}
