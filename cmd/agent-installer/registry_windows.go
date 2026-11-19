//go:build windows

package main

import "golang.org/x/sys/windows/registry"

const autostartKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func setAutostart(path string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, autostartKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(runValueName, `"`+path+`"`)
}

func removeAutostart() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, autostartKey, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	return key.DeleteValue(runValueName)
}
