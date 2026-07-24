package historyscan

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"tunnelctl/internal/aliasgen"
	"tunnelctl/internal/config"
)

type Candidate struct {
	Command        string
	Source         string
	Line           int
	SourceModified time.Time
	Profile        config.Profile
	Seen           int
}

var zshPrefix = regexp.MustCompile(`^:\s*\d+:\d+;`)

// Scan ищет команды ssh/autossh с -D в известных историях команд и пользовательских меню.
func Scan(existing config.Config) ([]Candidate, error) {
	used := usedNames(existing)
	dedup := map[string]*Candidate{}
	for _, path := range historyFiles() {
		_ = scanTextHistory(path, existing, used, dedup)
	}
	_ = scanPlatformSources(existing, used, dedup)

	out := make([]Candidate, 0, len(dedup))
	for _, candidate := range dedup {
		out = append(out, *candidate)
	}
	sortCandidates(out)
	return out, nil
}

func usedNames(existing config.Config) map[string]bool {
	used := map[string]bool{}
	for _, profile := range existing.Profiles {
		if profile.Name != "" {
			used[profile.Name] = true
		}
		if profile.Alias != "" {
			used[profile.Alias] = true
		}
	}
	for _, group := range existing.Groups {
		if group.Name != "" {
			used[group.Name] = true
		}
		if group.Alias != "" {
			used[group.Alias] = true
		}
	}
	return used
}

func sortCandidates(candidates []Candidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if !left.SourceModified.Equal(right.SourceModified) {
			return left.SourceModified.After(right.SourceModified)
		}
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		if left.Line != right.Line {
			return left.Line > right.Line
		}
		return dedupKey(left.Profile) < dedupKey(right.Profile)
	})
}

func historyFiles() []string {
	home, _ := os.UserHomeDir()
	var files []string
	if runtime.GOOS == "windows" {
		if app := os.Getenv("APPDATA"); app != "" {
			files = append(files, filepath.Join(app, "Microsoft", "Windows", "PowerShell", "PSReadLine", "ConsoleHost_history.txt"))
		}
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			files = append(files, filepath.Join(local, "Microsoft", "Windows", "PowerShell", "PSReadLine", "ConsoleHost_history.txt"))
		}
		return uniquePaths(files)
	}
	files = append(files,
		filepath.Join(home, ".bash_history"),
		filepath.Join(home, ".zsh_history"),
		filepath.Join(home, ".histfile"),
		filepath.Join(home, ".local", "share", "fish", "fish_history"),
	)
	return uniquePaths(files)
}

func scanTextHistory(path string, existing config.Config, used map[string]bool, dedup map[string]*Candidate) error {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	modified := fileModTime(file)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		line = zshPrefix.ReplaceAllString(line, "")
		if strings.HasPrefix(line, "- cmd: ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "- cmd: "))
		}
		addCommand(line, path, lineNo, modified, existing, used, dedup)
	}
	return scanner.Err()
}

func fileModTime(file *os.File) time.Time {
	if info, err := file.Stat(); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

func pathModTime(path string) time.Time {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

func addCommand(command, source string, order int, modified time.Time, existing config.Config, used map[string]bool, dedup map[string]*Candidate) {
	command = normalizeCommand(command)
	if command == "" || !containsSSH(command) {
		return
	}
	profile, ok := ParseSSHCommand(command)
	if !ok || profileExists(existing, profile) {
		return
	}

	key := dedupKey(profile)
	if old, exists := dedup[key]; exists {
		old.Seen++
		if occurrenceNewer(modified, source, order, *old) {
			old.Command = command
			old.Source = source
			old.Line = order
			old.SourceModified = modified
			old.Profile.Source = source
		}
		return
	}

	generated := aliasgen.Generate(aliasgen.Input{User: profile.User, Host: profile.Host, Key: profile.Key}, used)
	profile.Name = generated.Name
	profile.Alias = generated.Alias
	profile.Source = source
	dedup[key] = &Candidate{
		Command:        command,
		Source:         source,
		Line:           order,
		SourceModified: modified,
		Profile:        profile,
		Seen:           1,
	}
}

func normalizeCommand(command string) string {
	command = strings.TrimSpace(command)
	command = strings.TrimPrefix(command, "@")
	return strings.TrimSpace(command)
}

func containsSSH(command string) bool {
	lower := strings.ToLower(command)
	return strings.Contains(lower, "ssh") && strings.Contains(lower, "-d")
}

func occurrenceNewer(modified time.Time, source string, line int, old Candidate) bool {
	if !modified.Equal(old.SourceModified) {
		return modified.After(old.SourceModified)
	}
	if source != old.Source {
		return source < old.Source
	}
	return line > old.Line
}

func profileExists(cfg config.Config, profile config.Profile) bool {
	for _, existing := range cfg.Profiles {
		if existing.User == profile.User && existing.Host == profile.Host && effectivePort(existing.Port) == effectivePort(profile.Port) && existing.Key == profile.Key && existing.EffectiveListen(cfg) == profile.EffectiveListen(cfg) {
			return true
		}
	}
	return false
}

func effectivePort(port int) int {
	if port == 0 {
		return 22
	}
	return port
}

func dedupKey(profile config.Profile) string {
	return strings.Join([]string{profile.User, profile.Host, fmt.Sprint(effectivePort(profile.Port)), profile.Key, profile.Listen}, "|")
}

func uniquePaths(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "." || path == "" {
			continue
		}
		key := strings.ToLower(path)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, path)
	}
	return out
}

// ParseSSHCommand извлекает профиль из команды ssh/autossh с динамическим SOCKS-пробросом.
func ParseSSHCommand(command string) (config.Profile, bool) {
	tokens, err := splitShell(command)
	if err != nil || len(tokens) == 0 {
		return config.Profile{}, false
	}
	index := -1
	for i, token := range tokens {
		base := commandBase(token)
		if base == "ssh" || base == "autossh" {
			index = i
			break
		}
	}
	if index < 0 {
		return config.Profile{}, false
	}

	tokens = tokens[index+1:]
	profile := config.Profile{Port: 22}
	var target string
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if token == "--" {
			if i+1 < len(tokens) {
				target = tokens[i+1]
			}
			break
		}
		if strings.HasPrefix(token, "-") {
			switch {
			case token == "-D":
				if i+1 >= len(tokens) {
					return config.Profile{}, false
				}
				profile.Listen = normalizeListen(tokens[i+1])
				i++
			case strings.HasPrefix(token, "-D") && len(token) > 2:
				profile.Listen = normalizeListen(token[2:])
			case token == "-i":
				if i+1 < len(tokens) {
					profile.Key = tokens[i+1]
					i++
				}
			case strings.HasPrefix(token, "-i") && len(token) > 2:
				profile.Key = token[2:]
			case token == "-p":
				if i+1 < len(tokens) {
					profile.Port = atoiDefault(tokens[i+1], 22)
					i++
				}
			case strings.HasPrefix(token, "-p") && len(token) > 2:
				profile.Port = atoiDefault(token[2:], 22)
			case token == "-l":
				if i+1 < len(tokens) {
					profile.User = tokens[i+1]
					i++
				}
			case token == "-o":
				if i+1 < len(tokens) {
					applyOption(&profile, tokens[i+1])
					i++
				}
			case strings.HasPrefix(token, "-o") && len(token) > 2:
				applyOption(&profile, token[2:])
			case token == "-N" || token == "-f" || token == "-T" || strings.HasPrefix(token, "-v") || token == "-4" || token == "-6":
				// Старые управляющие опции внешнего ssh нам не нужны.
			default:
				if needsArg(token) && i+1 < len(tokens) {
					i++
				}
			}
			continue
		}
		target = token
	}
	if profile.Listen == "" || target == "" {
		return config.Profile{}, false
	}
	parseTarget(target, &profile)
	if profile.User == "" || profile.Host == "" {
		return config.Profile{}, false
	}
	return profile, true
}

func commandBase(token string) string {
	if index := strings.LastIndexAny(token, `/\`); index >= 0 {
		token = token[index+1:]
	}
	token = strings.ToLower(token)
	return strings.TrimSuffix(token, ".exe")
}

func applyOption(profile *config.Profile, option string) {
	parts := strings.SplitN(option, "=", 2)
	if len(parts) != 2 {
		return
	}
	name := strings.ToLower(strings.TrimSpace(parts[0]))
	value := strings.TrimSpace(parts[1])
	switch name {
	case "hostname":
		profile.Host = value
	case "user":
		profile.User = value
	case "port":
		profile.Port = atoiDefault(value, 22)
	case "identityfile":
		profile.Key = value
	}
}

func parseTarget(target string, profile *config.Profile) {
	if at := strings.LastIndex(target, "@"); at >= 0 {
		if profile.User == "" {
			profile.User = target[:at]
		}
		target = target[at+1:]
	}
	if profile.Host == "" {
		profile.Host = strings.Trim(target, "[]")
	}
}

func normalizeListen(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Count(value, ":") == 0 {
		return "127.0.0.1:" + value
	}
	if strings.HasPrefix(value, ":") {
		return "127.0.0.1" + value
	}
	return value
}

func atoiDefault(value string, fallback int) int {
	var number int
	_, err := fmt.Sscanf(value, "%d", &number)
	if err != nil || number <= 0 {
		return fallback
	}
	return number
}

func needsArg(option string) bool {
	if len(option) != 2 || !strings.HasPrefix(option, "-") {
		return false
	}
	return strings.ContainsAny(option[1:], "bcDeFIJLmOQRSTUVWw")
}

func splitShell(value string) ([]string, error) {
	var out []string
	var token strings.Builder
	var quote rune
	runes := []rune(value)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\\' {
			if i+1 < len(runes) && shouldEscapeBackslash(quote, runes[i+1]) {
				i++
				token.WriteRune(runes[i])
			} else {
				token.WriteRune(r)
			}
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				token.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if token.Len() > 0 {
				out = append(out, token.String())
				token.Reset()
			}
			continue
		}
		token.WriteRune(r)
	}
	if quote != 0 {
		return nil, fmt.Errorf("незакрытая кавычка")
	}
	if token.Len() > 0 {
		out = append(out, token.String())
	}
	return out, nil
}

func shouldEscapeBackslash(quote, next rune) bool {
	if quote == '\'' {
		return false
	}
	if quote == '"' {
		return next == '"' || next == '\\'
	}
	return next == '\'' || next == '"' || next == '\\' || next == ' ' || next == '\t' || next == '\n' || next == '\r'
}
