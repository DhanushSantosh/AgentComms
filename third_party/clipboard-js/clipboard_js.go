// Copyright 2013 @atotto. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build js

package clipboard

import "errors"

// errUnavailable is returned by every read/write attempt in this build.
// There is no OS clipboard reachable from a browser sandbox (no xclip/xsel,
// no pbcopy/pbpaste, no Win32 clipboard API to bind to), so this build
// degrades honestly instead of pretending clipboard access exists.
var errUnavailable = errors.New("clipboard is not available in this build")

func init() {
	// Mirrors clipboard_unix.go's behavior of setting Unsupported when no
	// clipboard mechanism is found, so callers that check clipboard.Unsupported
	// before offering clipboard UI see the same honest signal here.
	Unsupported = true
}

func readAll() (string, error) {
	return "", errUnavailable
}

func writeAll(text string) error {
	return errUnavailable
}
