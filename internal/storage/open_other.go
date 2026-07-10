//go:build !windows

package storage

import "os"

func openLocalFile(path string) (*os.File, error) {
	return os.Open(path)
}

func replaceLocalFile(tempPath, destinationPath string) error {
	return os.Rename(tempPath, destinationPath)
}
