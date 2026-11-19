//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var procMessageBoxW = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")

func notify(title, message string) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	messagePtr, _ := syscall.UTF16PtrFromString(message)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(messagePtr)), uintptr(unsafe.Pointer(titlePtr)), 0x40)
}
