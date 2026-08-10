package main

import (
	"fmt"
	"os"

	"github.com/DhanushSantosh/AgentComms/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if _, ok := err.(*app.ExitError); !ok || !app.ContainsJSONFlag(os.Args[1:]) {
			fmt.Fprintln(os.Stderr, err)
		}
		if e, ok := err.(*app.ExitError); ok {
			os.Exit(e.Code)
		}
		os.Exit(1)
	}
}
