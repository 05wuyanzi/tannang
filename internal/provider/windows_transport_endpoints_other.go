// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build !windows

package provider

import (
	"errors"

	"github.com/05wuyanzi/tannang/internal/execution"
)

type unsupportedTransportTableAPI struct{}

func newPlatformTransportTableAPI() transportTableAPI { return unsupportedTransportTableAPI{} }

func (unsupportedTransportTableAPI) availability() (execution.Reason, error) {
	return execution.ReasonUnsupportedOS, errors.New("Windows transport endpoint tables require Windows")
}

func (unsupportedTransportTableAPI) getExtendedTcpTable([]byte, *uint32, bool, uint32, uint32) uint32 {
	return transportErrorNotSupported
}

func (unsupportedTransportTableAPI) getExtendedUdpTable([]byte, *uint32, bool, uint32, uint32) uint32 {
	return transportErrorNotSupported
}
