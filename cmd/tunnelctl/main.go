package main

import (
	"fmt"
	"os"

	"tunnelctl/internal/app"
	"tunnelctl/internal/elevation"
)

func main() {
	handled, exitCode, err := elevation.MaybeRelaunch(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
		os.Exit(1)
	}
	if handled {
		os.Exit(exitCode)
	}
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
		os.Exit(1)
	}
}
