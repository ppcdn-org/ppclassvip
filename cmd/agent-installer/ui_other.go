//go:build !windows

package main

import "log"

func notify(title, message string) {
	log.Printf("[%s] %s", title, message)
}
