// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build !windows

package provider

import (
	"context"
	"testing"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
)

func TestProcessIdentitySnapshotNonWindowsStubFailsClosed(t *testing.T) {
	t.Parallel()
	runner := NewProcessIdentitySnapshotRunner()
	descriptor := runner.Descriptor()
	if descriptor.Requirements.Available || descriptor.Requirements.AvailabilityReason != execution.ReasonUnsupportedOS {
		t.Fatalf("non-Windows Requirements = %+v, want unavailable/UNSUPPORTED_OS", descriptor.Requirements)
	}
	writer := &countingWriter{}
	result := runner.ExecuteTo(context.Background(), capability.ProcessIdentitySnapshot(), testProcessTarget("amd64"), writer)
	requireProcessResult(t, result, execution.Blocked, execution.ReasonUnsupportedOS)
	if writer.calls != 0 {
		t.Fatalf("non-Windows stub wrote %d times", writer.calls)
	}
}
