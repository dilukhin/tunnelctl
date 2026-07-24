package commanddiag

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"tunnelctl/internal/logx"
)

type Metadata struct {
	Context string
	Profile string
	Address string
}

func SafeCommand(name string, args []string, secrets ...string) string {
	masked := MaskArgs(args, secrets...)
	if len(masked) == 0 {
		return name
	}
	return name + " " + strings.Join(masked, " ")
}

func MaskArgs(args []string, secrets ...string) []string {
	out := append([]string(nil), args...)
	for i := 0; i < len(out); i++ {
		lower := strings.ToLower(out[i])
		if sensitiveFlag(lower) && i+1 < len(out) {
			out[i+1] = "<скрыто>"
			i++
			continue
		}
		for _, prefix := range []string{"--password=", "--token=", "--secret=", "--api-key=", "--identity-file="} {
			if strings.HasPrefix(lower, prefix) {
				out[i] = out[i][:len(prefix)] + "<скрыто>"
				break
			}
		}
	}
	for i := range out {
		out[i] = Sanitize(out[i], secrets...)
	}
	return out
}

func Sanitize(value string, secrets ...string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		value = strings.ReplaceAll(value, secret, "<скрыто>")
	}
	return value
}

func LogStart(meta Metadata, name string, args []string, secrets ...string) {
	logx.Info("внешняя команда: context=%q profile=%q address=%q command=%q", meta.Context, meta.Profile, meta.Address, SafeCommand(name, args, secrets...))
}

func LogFailure(meta Metadata, name string, args []string, err error, stderr string, secrets ...string) {
	logx.Error(
		"внешняя команда завершилась: context=%q profile=%q address=%q command=%q exit_code=%s error=%q stderr=%q",
		meta.Context,
		meta.Profile,
		meta.Address,
		SafeCommand(name, args, secrets...),
		ExitCode(err),
		Sanitize(errorText(err), secrets...),
		Sanitize(strings.TrimSpace(stderr), secrets...),
	)
}

func ExitCode(err error) string {
	if err == nil {
		return "0"
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return strconv.Itoa(exitErr.ExitCode())
	}
	return "не определён"
}

func sensitiveFlag(flag string) bool {
	switch flag {
	case "-i", "--identity-file", "--password", "--token", "--secret", "--api-key":
		return true
	default:
		return false
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}
