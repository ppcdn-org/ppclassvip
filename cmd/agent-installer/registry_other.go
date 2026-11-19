//go:build !windows

package main

func setAutostart(path string) error { return nil }

func removeAutostart() error { return nil }
