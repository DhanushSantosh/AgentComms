package main

import (
	"fmt"
	"os"

	"github.com/DhanushSantosh/AgentComms/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		exitError, isExitError := err.(*app.ExitError)
		if !isExitError || !exitError.Reported {
			fmt.Fprintln(os.Stderr, err)
		}
		if isExitError {
			os.Exit(exitError.Code)
		}
		os.Exit(1)
	}
}
