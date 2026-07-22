package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"tunnelctl/internal/app"
	"tunnelctl/internal/bootstrap"
	"tunnelctl/internal/elevation"
)

func main() {
	args := os.Args[1:]
	handled, exitCode, err := elevation.MaybeRelaunch(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
		os.Exit(1)
	}
	if handled {
		os.Exit(exitCode)
	}
	if len(args) > 0 && args[0] == "import" {
		configPath, parseErr := parseImportArgs(args[1:])
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "Ошибка: %v\n", parseErr)
			os.Exit(1)
		}
		if err := bootstrap.RunImport(configPath); err != nil {
			fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if err := app.Run(args); err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
		os.Exit(1)
	}
}

func parseImportArgs(args []string) (string, error) {
	configPath := ""
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--config" || argument == "-config":
			if index+1 >= len(args) {
				return "", errors.New("после --config нужен путь")
			}
			configPath = args[index+1]
			index++
		case strings.HasPrefix(argument, "--config="):
			configPath = strings.TrimPrefix(argument, "--config=")
		case strings.HasPrefix(argument, "-config="):
			configPath = strings.TrimPrefix(argument, "-config=")
		default:
			return "", fmt.Errorf("лишний аргумент import: %s", argument)
		}
	}
	return configPath, nil
}
