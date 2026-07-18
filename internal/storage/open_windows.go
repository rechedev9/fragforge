//go:build windows

package storage

import (
	"errors"
	"os"
	"syscall"
	"time"
	"unsafe"
)

var replaceFileW = syscall.NewLazyDLL("kernel32.dll").NewProc("ReplaceFileW")

const (
	windowsAccessDenied     syscall.Errno = 5
	windowsSharingViolation syscall.Errno = 32
)

func openLocalFile(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	var handle syscall.Handle
	for attempt := 0; attempt < 5; attempt++ {
		handle, err = syscall.CreateFile(
			name,
			syscall.GENERIC_READ,
			syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
			nil,
			syscall.OPEN_EXISTING,
			syscall.FILE_ATTRIBUTE_NORMAL,
			0,
		)
		if err == nil || (!errors.Is(err, windowsSharingViolation) && !errors.Is(err, windowsAccessDenied)) {
			break
		}
		// ReplaceFileW is atomic, but virus scanners and filesystem filters can
		// briefly deny a new handle while a generation JSON is being replaced.
		// Polling endpoints should not turn that millisecond-scale window into a
		// terminal 500 response.
		time.Sleep(time.Duration(attempt+1) * time.Millisecond)
	}
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = syscall.CloseHandle(handle)
		return nil, &os.PathError{Op: "open", Path: path, Err: syscall.EINVAL}
	}
	return file, nil
}

func replaceLocalFile(tempPath, destinationPath string) error {
	destination, err := syscall.UTF16PtrFromString(destinationPath)
	if err != nil {
		return err
	}
	temp, err := syscall.UTF16PtrFromString(tempPath)
	if err != nil {
		return err
	}
	replaced, _, callErr := replaceFileW.Call(
		uintptr(unsafe.Pointer(destination)),
		uintptr(unsafe.Pointer(temp)),
		0,
		0,
		0,
		0,
	)
	if replaced != 0 {
		return nil
	}
	if errors.Is(callErr, syscall.ERROR_FILE_NOT_FOUND) || errors.Is(callErr, syscall.ERROR_PATH_NOT_FOUND) {
		return os.Rename(tempPath, destinationPath)
	}
	if callErr != syscall.Errno(0) {
		return callErr
	}
	return syscall.EINVAL
}
