//go:build !windows

package snapshot

import (
	"errors"
	"os"
)

func syncDirectory(path string) (err error) {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, dir.Close()) }()
	return dir.Sync()
}
