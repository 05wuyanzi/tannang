// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build windows && 386

package provider

import "unsafe"

var _ [556]byte = [unsafe.Sizeof(processEntry32W{})]byte{}
var _ [12]byte = [unsafe.Offsetof(processEntry32W{}.DefaultHeapID)]byte{}
var _ [16]byte = [unsafe.Offsetof(processEntry32W{}.ModuleID)]byte{}
var _ [24]byte = [unsafe.Offsetof(processEntry32W{}.ParentProcessID)]byte{}
var _ [36]byte = [unsafe.Offsetof(processEntry32W{}.ExeFile)]byte{}
