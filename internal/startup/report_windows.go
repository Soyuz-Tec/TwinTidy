//go:build windows

package startup

import (
	"syscall"
	"unsafe"
)

const (
	messageBoxOK        = 0x00000000
	messageBoxIconError = 0x00000010
)

var messageBoxW = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")

func ReportFatal(title string, message string) {
	titlePtr, titleErr := syscall.UTF16PtrFromString(title)
	messagePtr, messageErr := syscall.UTF16PtrFromString(message)
	if titleErr != nil || messageErr != nil {
		return
	}
	_, _, _ = messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(messagePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		messageBoxOK|messageBoxIconError,
	)
}
