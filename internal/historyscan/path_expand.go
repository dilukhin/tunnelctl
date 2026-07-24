package historyscan

import (
	"os"
	"regexp"
	"strings"
)

var percentEnvironment = regexp.MustCompile(`%([^%]+)%`)

func expandWindowsEnvironment(value string, overrides map[string]string) string {
	environment := map[string]string{}
	for _, item := range os.Environ() {
		name, data, ok := strings.Cut(item, "=")
		if ok {
			environment[strings.ToUpper(name)] = data
		}
	}
	for name, data := range overrides {
		environment[strings.ToUpper(name)] = data
	}
	return percentEnvironment.ReplaceAllStringFunc(value, func(match string) string {
		parts := percentEnvironment.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		if data, ok := environment[strings.ToUpper(parts[1])]; ok {
			return data
		}
		return match
	})
}
