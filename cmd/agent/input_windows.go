//go:build windows

package main

import (
	"errors"
	"syscall"
	"unsafe"
)

var (
	user32        = syscall.NewLazyDLL("user32.dll")
	procSendInput = user32.NewProc("SendInput")
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	mouseMove        = 0x0001
	mouseLeftDown    = 0x0002
	mouseLeftUp      = 0x0004
	mouseRightDown   = 0x0008
	mouseRightUp     = 0x0010
	mouseMiddleDown  = 0x0020
	mouseMiddleUp    = 0x0040
	mouseWheel       = 0x0800
	mouseAbsolute    = 0x8000
	mouseVirtualDesk = 0x4000

	keyEventKeyUp = 0x0002

	sendInputSize = unsafe.Sizeof(input{})
)

type mouseInput struct {
	dx        int32
	dy        int32
	mouseData uint32
	flags     uint32
	time      uint32
	extraInfo uintptr
}

type keyboardInput struct {
	vk        uint16
	scan      uint16
	flags     uint32
	time      uint32
	extraInfo uintptr
}

type input struct {
	typ  uint32
	_    uint32
	data [32]byte
}

func injectEvent(event controlEvent) error {
	switch event.Type {
	case "mouse.move":
		return sendMouseMove(event.X, event.Y)
	case "mouse.button":
		return sendMouseButton(event.Button, event.Action)
	case "mouse.wheel":
		return sendMouseWheel(int32(event.Dy), int32(event.Dx))
	case "key":
		return sendKey(event.Code, event.Action)
	default:
		return nil
	}
}

func sendMouseMove(x, y float64) error {
	if x < 0 {
		x = 0
	}
	if x > 1 {
		x = 1
	}
	if y < 0 {
		y = 0
	}
	if y > 1 {
		y = 1
	}
	in := input{typ: inputMouse}
	mi := (*mouseInput)(unsafe.Pointer(&in.data[0]))
	mi.dx = int32(x * 65535)
	mi.dy = int32(y * 65535)
	mi.flags = mouseMove | mouseAbsolute | mouseVirtualDesk
	return sendInput(&in)
}

func sendMouseButton(button, action string) error {
	var down, up uint32
	switch button {
	case "right":
		down, up = mouseRightDown, mouseRightUp
	case "middle":
		down, up = mouseMiddleDown, mouseMiddleUp
	default:
		down, up = mouseLeftDown, mouseLeftUp
	}
	flags := down
	if action == "up" {
		flags = up
	}
	in := input{typ: inputMouse}
	mi := (*mouseInput)(unsafe.Pointer(&in.data[0]))
	mi.flags = flags
	return sendInput(&in)
}

func sendMouseWheel(vertical, horizontal int32) error {
	if vertical != 0 {
		in := input{typ: inputMouse}
		mi := (*mouseInput)(unsafe.Pointer(&in.data[0]))
		mi.flags = mouseWheel
		mi.mouseData = uint32(vertical)
		if err := sendInput(&in); err != nil {
			return err
		}
	}
	if horizontal != 0 {
		in := input{typ: inputMouse}
		mi := (*mouseInput)(unsafe.Pointer(&in.data[0]))
		mi.flags = 0x01000 // MOUSEEVENTF_HWHEEL
		mi.mouseData = uint32(horizontal)
		return sendInput(&in)
	}
	return nil
}

func sendKey(code, action string) error {
	vk, ok := virtualKey(code)
	if !ok {
		return nil
	}
	flags := uint32(0)
	if action == "up" {
		flags = keyEventKeyUp
	}
	in := input{typ: inputKeyboard}
	ki := (*keyboardInput)(unsafe.Pointer(&in.data[0]))
	ki.vk = vk
	ki.flags = flags
	return sendInput(&in)
}

func sendInput(in *input) error {
	r1, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(in)), sendInputSize)
	if r1 == 0 {
		if err != nil && err != syscall.Errno(0) {
			return err
		}
		return errors.New("SendInput returned 0")
	}
	return nil
}

func virtualKey(code string) (uint16, bool) {
	if len(code) == 4 && code[:3] == "Key" {
		c := code[3]
		if c >= 'A' && c <= 'Z' {
			return uint16(c), true
		}
	}
	if len(code) == 6 && code[:5] == "Digit" {
		c := code[5]
		if c >= '0' && c <= '9' {
			return uint16(c), true
		}
	}
	if len(code) == 7 && code[:6] == "Numpad" {
		c := code[6]
		if c >= '0' && c <= '9' {
			return uint16(c-'0') + 0x60, true
		}
	}
	if len(code) >= 2 && code[0] == 'F' {
		var n int
		for i := 1; i < len(code); i++ {
			if code[i] < '0' || code[i] > '9' {
				n = 0
				break
			}
			n = n*10 + int(code[i]-'0')
		}
		if n >= 1 && n <= 12 {
			return uint16(0x70 + n - 1), true
		}
	}
	keys := map[string]uint16{
		"Escape": 0x1B, "Enter": 0x0D, "Tab": 0x09, "Backspace": 0x08,
		"Space": 0x20, "Delete": 0x2E, "Insert": 0x2D, "Home": 0x24,
		"End": 0x23, "PageUp": 0x21, "PageDown": 0x22,
		"ArrowLeft": 0x25, "ArrowUp": 0x26, "ArrowRight": 0x27, "ArrowDown": 0x28,
		"ShiftLeft": 0x10, "ShiftRight": 0x10, "ControlLeft": 0x11, "ControlRight": 0x11,
		"AltLeft": 0x12, "AltRight": 0x12, "MetaLeft": 0x5B, "MetaRight": 0x5C,
		"CapsLock": 0x14, "Minus": 0xBD, "Equal": 0xBB, "BracketLeft": 0xDB,
		"BracketRight": 0xDD, "Backslash": 0xDC, "Semicolon": 0xBA, "Quote": 0xDE,
		"Backquote": 0xC0, "Comma": 0xBC, "Period": 0xBE, "Slash": 0xBF,
	}
	if vk, ok := keys[code]; ok {
		return vk, true
	}
	return 0, false
}
