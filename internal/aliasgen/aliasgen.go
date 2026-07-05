package aliasgen

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type Input struct {
	User string
	Host string
	Key  string
}

type Result struct {
	Name  string
	Alias string
}

var nonSafe = regexp.MustCompile(`[^a-z0-9]+`)
var ipRe = regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)

var generic = map[string]bool{
	"id": true, "ssh": true, "key": true, "rsa": true, "ed25519": true, "ecdsa": true,
	"private": true, "default": true, "new": true, "old": true,
}

// Generate создаёт человекочитаемые имя профиля и алиас.
func Generate(in Input, used map[string]bool) Result {
	base := bestBase(in)
	if base == "" {
		base = "tunnel"
	}
	alias := uniqueAlias(base, in.Host, used)
	name := makeName(base, in.Host)
	name = uniqueName(name, used)
	return Result{Name: name, Alias: alias}
}

func bestBase(in Input) string {
	if k := baseFromKey(in.Key); k != "" {
		return k
	}
	if h := baseFromHost(in.Host); h != "" {
		return h
	}
	if u := cleanup(in.User); u != "" {
		if strings.Contains(u, "tunnel") {
			return "tunnel"
		}
		return u
	}
	return "tunnel"
}

func baseFromKey(key string) string {
	if key == "" {
		return ""
	}
	b := filepath.Base(key)
	b = strings.TrimPrefix(b, "id_")
	b = strings.TrimPrefix(b, "ssh_")
	b = strings.TrimSuffix(b, "_ed25519")
	b = strings.TrimSuffix(b, "_rsa")
	b = strings.TrimSuffix(b, "_ecdsa")
	parts := strings.FieldsFunc(b, func(r rune) bool { return r == '_' || r == '-' || r == '.' })
	filtered := make([]string, 0, len(parts))
	for _, p := range parts {
		p = cleanup(p)
		if p == "" || generic[p] {
			continue
		}
		filtered = append(filtered, p)
	}
	if len(filtered) == 0 {
		return ""
	}
	if len(filtered) >= 2 && filtered[0] == "yandex" && filtered[1] == "vm" {
		return "yandex"
	}
	return strings.Join(filtered, "-")
}

func baseFromHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return ""
	}
	if ipRe.MatchString(host) {
		parts := strings.Split(host, ".")
		if len(parts) >= 2 {
			return "ip-" + parts[0] + "-" + parts[1]
		}
		return "ip"
	}
	parts := strings.Split(host, ".")
	if len(parts) > 0 {
		return cleanup(parts[0])
	}
	return cleanup(host)
}

func makeName(base, host string) string {
	h := strings.ReplaceAll(strings.ToLower(host), ".", "-")
	h = cleanup(h)
	if h == "" {
		return base
	}
	if strings.HasPrefix(base, "ip-") {
		return h
	}
	return cleanup(base + "-" + h)
}

func uniqueAlias(base, host string, used map[string]bool) string {
	candidates := []string{cleanup(base)}
	if ipRe.MatchString(host) {
		parts := strings.Split(host, ".")
		if len(parts) > 0 {
			candidates = append(candidates, cleanup(base+"-"+parts[0]))
		}
	}
	for _, c := range candidates {
		if c != "" && !used[c] {
			used[c] = true
			return c
		}
	}
	base = cleanup(base)
	for i := 2; ; i++ {
		c := fmt.Sprintf("%s-%d", base, i)
		if !used[c] {
			used[c] = true
			return c
		}
	}
}

func uniqueName(base string, used map[string]bool) string {
	base = cleanup(base)
	if base == "" {
		base = "tunnel"
	}
	if !used[base] {
		used[base] = true
		return base
	}
	for i := 2; ; i++ {
		c := fmt.Sprintf("%s-%d", base, i)
		if !used[c] {
			used[c] = true
			return c
		}
	}
}

func cleanup(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = nonSafe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
