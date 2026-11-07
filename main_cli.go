//go:build cli

package main

import (
	"os"

	"app/internal/cli"
)

// photos0x-cli is built without Wails, so it is CLI only.
func main() {
	os.Exit(cli.Run(os.Args[1:], cli.Info{
		ExecutableName: "photos0x-cli",
		HasGUI:         false,
		Version:        getAppVersion(),
	}))
}
