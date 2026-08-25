// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If applicable, see <http://mozilla.org/MPL/2.0/>.

//go:build !windows

package provider

import (
	"errors"

	"github.com/05wuyanzi/tannang/internal/execution"
)

type unsupportedHostOSIdentityAPI struct{}

func newPlatformHostOSIdentityAPI() hostOSIdentityAPI { return unsupportedHostOSIdentityAPI{} }
func (unsupportedHostOSIdentityAPI) availability() (execution.Reason, error) {
	return execution.ReasonUnsupportedOS, errors.New("Windows host identity requires Windows")
}
func (unsupportedHostOSIdentityAPI) computerNameEx([]uint16, *uint32) (bool, error) {
	return false, errors.New("unsupported platform")
}
func (unsupportedHostOSIdentityAPI) osVersion() (uint32, uint32, uint32, error) {
	return 0, 0, 0, errors.New("unsupported platform")
}
func (unsupportedHostOSIdentityAPI) nativeArchitecture() (string, error) {
	return "", errors.New("unsupported platform")
}
