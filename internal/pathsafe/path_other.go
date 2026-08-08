// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build !windows

package pathsafe

import (
	"os"
	"path/filepath"
	"strings"
)

func platformValidateAbsoluteLocalPath(input string) (string, error) {
	if input == "" {
		return "", reject(CodeUnsafePath, "validate path", "path must be explicitly specified")
	}
	if strings.HasPrefix(input, "//") {
		return "", reject(CodeNetworkPathUnsupported, "validate path", "network paths are unsupported")
	}
	if !filepath.IsAbs(input) || filepath.Clean(input) != input {
		return "", reject(CodeUnsafePath, "validate path", "path must be absolute and canonical")
	}
	return input, nil
}

func platformValidateExistingPath(path string) error {
	abs, err := platformValidateAbsoluteLocalPath(path)
	if err != nil {
		return err
	}
	current := string(filepath.Separator)
	for _, component := range strings.Split(strings.TrimPrefix(abs, current), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return rejectCause(CodeUnsafePath, "inspect filesystem path", "path component cannot be inspected", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return reject(CodeReparsePointDetected, "inspect filesystem path", "a protected path component is a symbolic link")
		}
	}
	return nil
}

func platformValidateRelativeComponent(component string) error {
	if strings.ContainsRune(component, 0) {
		return reject(CodeUnsafePath, "validate child path", "path component contains NUL")
	}
	return nil
}

func platformRenameNoReplace(source, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return os.ErrExist
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(source, destination)
}

func platformIsRetryableRenameError(error) bool {
	return false
}
