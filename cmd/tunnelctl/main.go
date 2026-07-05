package main

import (
	"fmt"
	"os"

	"tunnelctl/internal/app"
)

var version = "0.1.0-mvp"

func main() {
	if err := app.Run(os.Args[1:], version); err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
		os.Exit(1)
	}
}
