//go:build !windows

package main

import (
	"fmt"
	"os"
)

func runUI(app *agentApp) error {
	return runConsoleUI(app)
}

func showMessage(title, message string) {
	fmt.Printf("[%s] %s\n", title, message)
}

func confirmConsent(teacherName, mode string) bool {
	fmt.Printf("%s requested remote assist (%s). Allow? [Y/n] ", teacherName, mode)
	var answer string
	_, _ = fmt.Scanln(&answer)
	return answer == "" || answer == "y" || answer == "Y"
}

func promptPairingCode() (string, error) {
	fmt.Print("Pairing code: ")
	var code string
	_, err := fmt.Scanln(&code)
	return code, err
}

var _ = os.Stdout
