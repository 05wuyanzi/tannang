// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package pathsafe

import (
	"errors"
	"fmt"
	"testing"
)

func TestHasCodeTraversesWrappedAndJoinedErrors(t *testing.T) {
	unsafe := reject(CodeUnsafePath, "test", "unsafe")
	reparse := reject(CodeReparsePointDetected, "test", "reparse")
	joined := errors.Join(unsafe, reparse)
	cases := []struct {
		name string
		err  error
		code Code
		want bool
	}{
		{"direct", unsafe, CodeUnsafePath, true},
		{"wrapped", fmt.Errorf("wrapped: %w", unsafe), CodeUnsafePath, true},
		{"joined unsafe", joined, CodeUnsafePath, true},
		{"joined reparse", joined, CodeReparsePointDetected, true},
		{"joined absent", joined, CodeNetworkPathUnsupported, false},
		{"nested wrap join", fmt.Errorf("outer: %w", errors.Join(fmt.Errorf("inner: %w", unsafe), reparse)), CodeReparsePointDetected, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasCode(tc.err, tc.code); got != tc.want {
				t.Fatalf("HasCode(%v, %s) = %t, want %t", tc.err, tc.code, got, tc.want)
			}
		})
	}
}
