// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package syntheticfixtures exposes only fixed files embedded at build time.
package syntheticfixtures

import "embed"

//go:embed *.json
var files embed.FS

// Read returns a named embedded fixture. Callers must validate name syntax.
func Read(name string) ([]byte, error) {
	return files.ReadFile(name + ".json")
}
