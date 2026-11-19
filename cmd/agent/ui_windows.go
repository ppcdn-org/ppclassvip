//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

const (
	wmUser            = 0x0400
	wmApp             = 0x8000
	wmCommand         = 0x0111
	wmDestroy         = 0x0002
	wmNull            = 0x0000
	wmRButtonUp       = 0x0205
	wmLButtonDblClk   = 0x0203
	trayCallbackMsg   = wmApp + 1
	nimAdd            = 0
	nimDelete         = 2
	nifMessage        = 0x00000001
	nifIcon           = 0x00000002
	nifTip            = 0x00000004
	mfString          = 0x00000000
	tpmRightButton    = 0x0002
	tpmReturnCmd      = 0x0100
	idiApplication    = 32512
	mbOK              = 0x00000000
	mbYesNo           = 0x00000004
	mbIconQuestion    = 0x00000020
	mbIconInformation = 0x00000040
	idYes             = 6
)

type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

type wndClass struct {
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
}

type point struct {
	x int32
	y int32
}

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

var (
	trayUser32 = syscall.NewLazyDLL("user32.dll")
	shell32    = syscall.NewLazyDLL("shell32.dll")
	kernel32   = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassW      = user32.NewProc("RegisterClassW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procLoadIconW           = user32.NewProc("LoadIconW")
	procMessageBoxW         = user32.NewProc("MessageBoxW")
	procShellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
)

var (
	trayApp      *agentApp
	trayWindow   uintptr
	trayIconData notifyIconData
	wndProcFunc  uintptr
)

func runUI(app *agentApp) error {
	trayApp = app
	className, _ := syscall.UTF16PtrFromString("PPClassAgentTrayWindow")
	title, _ := syscall.UTF16PtrFromString("PPClass Agent")
	instance, _, _ := procGetModuleHandleW.Call(0)
	wndProcFunc = syscall.NewCallback(trayWindowProc)

	wc := wndClass{
		lpfnWndProc:   wndProcFunc,
		hInstance:     instance,
		lpszClassName: className,
	}
	if ret, _, err := procRegisterClassW.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		return fmt.Errorf("register tray window class: %v", err)
	}
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0,
		0, 0,
		instance,
		0,
	)
	if hwnd == 0 {
		return fmt.Errorf("create tray window: %v", err)
	}
	trayWindow = hwnd

	trayIconData = notifyIconData{
		cbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		hWnd:             hwnd,
		uID:              1,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: trayCallbackMsg,
	}
	icon, _, _ := procLoadIconW.Call(0, idiApplication)
	trayIconData.hIcon = icon
	copyUTF16(trayIconData.szTip[:], "PPClass 远程协助")
	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&trayIconData)))

	for {
		var message msg
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&trayIconData)))
	return nil
}

func trayWindowProc(hwnd, message, wParam, lParam uintptr) uintptr {
	switch message {
	case trayCallbackMsg:
		if lParam == wmRButtonUp || lParam == wmLButtonDblClk {
			showTrayMenu(hwnd)
		}
	case wmCommand:
		switch int(wParam & 0xffff) {
		case 1:
			go func() {
				code, err := promptPairingCode()
				if err != nil || strings.TrimSpace(code) == "" {
					return
				}
				trayApp.codeCh <- strings.ToUpper(strings.TrimSpace(code))
			}()
		case 2:
			go func() { _ = trayApp.applyUpdate() }()
		case 3:
			trayApp.disconnect()
		case 4:
			trayApp.disconnect()
			procDestroyWindow.Call(hwnd)
			procPostQuitMessage.Call(0)
			return 0
		}
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, message, wParam, lParam)
	return ret
}

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	appendMenu := func(id uintptr, text string) {
		ptr, _ := syscall.UTF16PtrFromString(text)
		procAppendMenuW.Call(menu, mfString, id, uintptr(unsafe.Pointer(ptr)))
	}
	appendMenu(1, "输入配对码")
	appendMenu(2, "检查更新")
	appendMenu(3, "断开当前会话")
	appendMenu(4, "退出")
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(hwnd)
	command, _, _ := procTrackPopupMenu.Call(
		menu,
		tpmReturnCmd|tpmRightButton,
		uintptr(pt.x),
		uintptr(pt.y),
		0,
		hwnd,
		0,
	)
	procPostMessageW.Call(hwnd, wmNull, 0, 0)
	if command != 0 {
		procPostMessageW.Call(hwnd, wmCommand, command, 0)
	}
}

func showMessage(title, message string) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	messagePtr, _ := syscall.UTF16PtrFromString(message)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(messagePtr)), uintptr(unsafe.Pointer(titlePtr)), mbOK|mbIconInformation)
}

func confirmConsent(teacherName, mode string) bool {
	titlePtr, _ := syscall.UTF16PtrFromString("PPClass 远程协助")
	modeText := "查看屏幕"
	if mode == "control" {
		modeText = "查看并控制屏幕"
	}
	text := fmt.Sprintf("%s 请求远程协助：%s。\n\n是否允许？\n\n允许后屏幕会开始上传，控制操作会通过本机注入。", teacherName, modeText)
	messagePtr, _ := syscall.UTF16PtrFromString(text)
	ret, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(messagePtr)), uintptr(unsafe.Pointer(titlePtr)), mbYesNo|mbIconQuestion)
	return ret == idYes
}

func promptPairingCode() (string, error) {
	script := `Add-Type -AssemblyName Microsoft.VisualBasic; [Microsoft.VisualBasic.Interaction]::InputBox('请输入课堂远程协助配对码','PPClass 远程协助','')`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-STA", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func copyUTF16(dst []uint16, value string) {
	encoded := utf16.Encode([]rune(value))
	if len(encoded) > len(dst)-1 {
		encoded = encoded[:len(dst)-1]
	}
	copy(dst, encoded)
	dst[len(encoded)] = 0
}
