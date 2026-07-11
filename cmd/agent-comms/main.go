package main

import (
	"fmt"
	"os"

	"github.com/DhanushSantosh/AgentComms/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if _, ok := err.(*app.ExitError); !ok || !containsJSONFlag(os.Args[1:]) {
			fmt.Fprintln(os.Stderr, err)
		}
		if e, ok := err.(*app.ExitError); ok {
			os.Exit(e.Code)
		}
		os.Exit(1)
	}
}

func containsJSONFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}
