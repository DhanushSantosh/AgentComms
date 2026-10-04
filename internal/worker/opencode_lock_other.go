//go:build js || plan9

package worker

import (
	"errors"
	"os"
)

func lockOpenCodeFile(string) (*os.File, error) {
	return nil, errors.New("owned OpenCode live workers are unsupported on this platform")
}

func unlockOpenCodeFile(file *os.File) error {
	if file == nil {
		return nil
	}
	return file.Close()
}
