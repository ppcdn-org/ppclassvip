//go:build !windows

package main

import "errors"

func injectEvent(controlEvent) error {
	return errors.New("input injection is only implemented on Windows")
}
