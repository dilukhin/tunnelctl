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

	"tunnelctl/internal/aliasgen"
	"tunnelctl/internal/config"
)

type Candidate struct {
	Command string
	Source  string
	Line    int
	Profile config.Profile
	Seen    int
}

var zshPrefix = regexp.MustCompile(`^:\s*\d+:\d+;`)

// Scan ищет команды ssh/autossh с -D в известных файлах истории.
func Scan(existing config.Config) ([]Candidate, error) {
	files := historyFiles()
	used := map[string]bool{}
	for _, p := range existing.Profiles {
		if p.Name != "" {
			used[p.Name] = true
		}
		if p.Alias != "" {
			used[p.Alias] = true
		}
	}

	dedup := map[string]*Candidate{}
	for _, f := range files {
		_ = scanFile(f, existing, used, dedup)
	}
	out := make([]Candidate, 0, len(dedup))
	for _, c := range dedup {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Source+fmt.Sprint(out[i].Line) < out[j].Source+fmt.Sprint(out[j].Line)
	})
	return out, nil
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
		return files
	}
	files = append(files,
		filepath.Join(home, ".bash_history"),
		filepath.Join(home, ".zsh_history"),
		filepath.Join(home, ".histfile"),
		filepath.Join(home, ".local", "share", "fish", "fish_history"),
	)
	return files
}

func scanFile(path string, existing config.Config, used map[string]bool, dedup map[string]*Candidate) error {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	lineNo := 0
	for s.Scan() {
		lineNo++
		line := strings.TrimSpace(s.Text())
		line = zshPrefix.ReplaceAllString(line, "")
		if strings.HasPrefix(line, "- cmd: ") { // fish_history
			line = strings.TrimSpace(strings.TrimPrefix(line, "- cmd: "))
		}
		if !strings.Contains(line, "ssh") || !strings.Contains(line, "-D") {
			continue
		}
		p, ok := ParseSSHCommand(line)
		if !ok {
			continue
		}
		if profileExists(existing, p) {
			continue
		}
		gen := aliasgen.Generate(aliasgen.Input{User: p.User, Host: p.Host, Key: p.Key}, used)
		p.Name = gen.Name
		p.Alias = gen.Alias
		p.Source = path
		key := dedupKey(p)
		if old, exists := dedup[key]; exists {
			old.Seen++
			continue
		}
		dedup[key] = &Candidate{Command: line, Source: path, Line: lineNo, Profile: p, Seen: 1}
	}
	return s.Err()
}

func profileExists(cfg config.Config, p config.Profile) bool {
	for _, e := range cfg.Profiles {
		if e.User == p.User && e.Host == p.Host && e.Port == p.Port && e.Key == p.Key {
			return true
		}
	}
	return false
}

func dedupKey(p config.Profile) string {
	return strings.Join([]string{p.User, p.Host, fmt.Sprint(p.Port), p.Key, p.Listen}, "|")
}

// ParseSSHCommand извлекает профиль из команды ssh/autossh с динамическим SOCKS-пробросом.
func ParseSSHCommand(command string) (config.Profile, bool) {
	tokens, err := splitShell(command)
	if err != nil || len(tokens) == 0 {
		return config.Profile{}, false
	}
	idx := -1
	for i, t := range tokens {
		base := filepath.Base(t)
		if base == "ssh" || base == "autossh" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return config.Profile{}, false
	}
	tokens = tokens[idx+1:]
	p := config.Profile{Port: 22}
	var target string
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if t == "--" {
			if i+1 < len(tokens) {
				target = tokens[i+1]
			}
			break
		}
		if strings.HasPrefix(t, "-") {
			switch {
			case t == "-D":
				if i+1 >= len(tokens) {
					return config.Profile{}, false
				}
				p.Listen = normalizeListen(tokens[i+1])
				i++
			case strings.HasPrefix(t, "-D") && len(t) > 2:
				p.Listen = normalizeListen(t[2:])
			case t == "-i":
				if i+1 < len(tokens) {
					p.Key = tokens[i+1]
					i++
				}
			case strings.HasPrefix(t, "-i") && len(t) > 2:
				p.Key = t[2:]
			case t == "-p":
				if i+1 < len(tokens) {
					p.Port = atoiDefault(tokens[i+1], 22)
					i++
				}
			case strings.HasPrefix(t, "-p") && len(t) > 2:
				p.Port = atoiDefault(t[2:], 22)
			case t == "-l":
				if i+1 < len(tokens) {
					p.User = tokens[i+1]
					i++
				}
			case t == "-o":
				if i+1 < len(tokens) {
					applyOption(&p, tokens[i+1])
					i++
				}
			case strings.HasPrefix(t, "-o") && len(t) > 2:
				applyOption(&p, t[2:])
			case t == "-N" || t == "-f" || t == "-T" || strings.HasPrefix(t, "-v") || t == "-4" || t == "-6":
				// Старые управляющие опции внешнего ssh нам не нужны.
			default:
				// Для неизвестной опции с аргументом грубо пропускаем следующий токен только для известных односимвольных форм.
				if needsArg(t) && i+1 < len(tokens) {
					i++
				}
			}
			continue
		}
		target = t
	}
	if p.Listen == "" || target == "" {
		return config.Profile{}, false
	}
	parseTarget(target, &p)
	if p.User == "" || p.Host == "" {
		return config.Profile{}, false
	}
	return p, true
}

func applyOption(p *config.Profile, opt string) {
	parts := strings.SplitN(opt, "=", 2)
	if len(parts) != 2 {
		return
	}
	name := strings.ToLower(strings.TrimSpace(parts[0]))
	value := strings.TrimSpace(parts[1])
	switch name {
	case "hostname":
		p.Host = value
	case "user":
		p.User = value
	case "port":
		p.Port = atoiDefault(value, 22)
	case "identityfile":
		p.Key = value
	}
}

func parseTarget(target string, p *config.Profile) {
	if at := strings.LastIndex(target, "@"); at >= 0 {
		if p.User == "" {
			p.User = target[:at]
		}
		target = target[at+1:]
	}
	if p.Host == "" {
		p.Host = strings.Trim(target, "[]")
	}
}

func normalizeListen(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.Count(v, ":") == 0 {
		return "127.0.0.1:" + v
	}
	if strings.HasPrefix(v, ":") {
		return "127.0.0.1" + v
	}
	return v
}

func atoiDefault(s string, def int) int {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func needsArg(opt string) bool {
	if len(opt) != 2 || !strings.HasPrefix(opt, "-") {
		return false
	}
	return strings.ContainsAny(opt[1:], "bcDeFIJLmOQRSTUVWw")
}

func splitShell(s string) ([]string, error) {
	var out []string
	var b strings.Builder
	var quote rune
	escaped := false
	for _, r := range s {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
			continue
		}
		b.WriteRune(r)
	}
	if escaped {
		b.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("незакрытая кавычка")
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out, nil
}
