package historyscan

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"tunnelctl/internal/config"
)

func scanFarMenu(path string, existing config.Config, used map[string]bool, dedup map[string]*Candidate) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	modified := pathModTime(path)
	scanner := bufio.NewScanner(strings.NewReader(decodeText(raw)))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if line == "" || !startsWithSpace(line) {
			continue
		}
		command := strings.TrimSpace(line)
		if command == "" || command == "{" || command == "}" {
			continue
		}
		addCommand(command, path, lineNo, modified, existing, used, dedup)
	}
	return scanner.Err()
}

func startsWithSpace(value string) bool {
	return len(value) > 0 && (value[0] == ' ' || value[0] == '\t')
}

func scanTotalCommanderINI(path string, existing config.Config, used map[string]bool, dedup map[string]*Candidate) error {
	return scanTotalCommanderINIRecursive(path, existing, used, dedup, map[string]bool{}, 0)
}

func scanTotalCommanderINIRecursive(path string, existing config.Config, used map[string]bool, dedup map[string]*Candidate, visited map[string]bool, depth int) error {
	if depth > 4 {
		return errors.New("слишком глубокая цепочка RedirectSection в Total Commander")
	}
	absolute, _ := filepath.Abs(path)
	key := strings.ToLower(filepath.Clean(absolute))
	if visited[key] {
		return nil
	}
	visited[key] = true

	entries, err := readINI(path)
	if err != nil {
		return err
	}
	modified := pathModTime(path)

	for _, entry := range entries {
		if entry.Section != "command line history" {
			continue
		}
		index, err := parseNonNegativeInt(entry.Key)
		if err != nil {
			continue
		}
		addCommand(entry.Value, path, 1_000_000-index, modified, existing, used, dedup)
	}

	scanTotalCommanderUserSection(path, entries, modified, existing, used, dedup)
	redirect := iniValue(entries, "user", "redirectsection")
	if redirect != "" {
		redirect = expandWindowsEnvironment(redirect, nil)
		if !filepath.IsAbs(redirect) {
			redirect = filepath.Join(filepath.Dir(path), redirect)
		}
		_ = scanTotalCommanderINIRecursive(redirect, existing, used, dedup, visited, depth+1)
	}
	return nil
}

func scanTotalCommanderUserSection(path string, entries []iniEntry, modified time.Time, existing config.Config, used map[string]bool, dedup map[string]*Candidate) {
	type menuCommand struct {
		command string
		params  string
		line    int
	}
	commands := map[int]*menuCommand{}
	for _, entry := range entries {
		if entry.Section != "user" {
			continue
		}
		if index, ok := parseNumericSuffix(entry.Key, "cmd"); ok {
			item := commands[index]
			if item == nil {
				item = &menuCommand{}
				commands[index] = item
			}
			item.command = entry.Value
			item.line = entry.Line
		} else if index, ok := parseNumericSuffix(entry.Key, "param"); ok {
			item := commands[index]
			if item == nil {
				item = &menuCommand{}
				commands[index] = item
			}
			item.params = entry.Value
			if item.line == 0 {
				item.line = entry.Line
			}
		}
	}
	indexes := make([]int, 0, len(commands))
	for index := range commands {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		item := commands[index]
		addCommand(joinCommand(item.command, item.params), path, item.line, modified, existing, used, dedup)
	}
}

func scanTotalCommanderUserCmd(path string, existing config.Config, used map[string]bool, dedup map[string]*Candidate) error {
	entries, err := readINI(path)
	if err != nil {
		return err
	}
	modified := pathModTime(path)
	type command struct {
		cmd    string
		params string
		line   int
	}
	sections := map[string]*command{}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Section, "em_") {
			continue
		}
		item := sections[entry.Section]
		if item == nil {
			item = &command{}
			sections[entry.Section] = item
		}
		switch entry.Key {
		case "cmd":
			item.cmd = entry.Value
			item.line = entry.Line
		case "param":
			item.params = entry.Value
			if item.line == 0 {
				item.line = entry.Line
			}
		}
	}
	names := make([]string, 0, len(sections))
	for name := range sections {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		item := sections[name]
		addCommand(joinCommand(item.cmd, item.params), path, item.line, modified, existing, used, dedup)
	}
	return nil
}

func joinCommand(command, params string) string {
	command = strings.TrimSpace(command)
	params = strings.TrimSpace(params)
	if command == "" {
		return params
	}
	if params == "" {
		return command
	}
	return command + " " + params
}

func parseNonNegativeInt(value string) (int, error) {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || number < 0 {
		return 0, errors.New("не число")
	}
	return number, nil
}
