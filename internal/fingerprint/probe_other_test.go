// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build !windows

package fingerprint

import (
	"context"
	"errors"
	"testing"
)

func TestNonWindowsProbeFailsClosed(t *testing.T) {
	target, err := Probe(context.Background(), "/tmp/tannang-output", Options{})
	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("Probe() error = %v, want ErrUnsupportedPlatform", err)
	}
	if target.Probe == nil || target.Probe.OSVersion.State != Unsupported || target.Probe.NativeArchitecture.State != Unsupported {
		t.Fatalf("non-Windows probe did not preserve unsupported field state: %+v", target.Probe)
	}
}
