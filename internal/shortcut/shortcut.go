package shortcut

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"tunnelctl/internal/config"
)

// CreateInteractive создаёт доступные для платформы ярлыки/скрипты запуска.
func CreateInteractive(in *bufio.Reader, cfg config.Config) error {
	switch runtime.GOOS {
	case "windows":
		return createWindowsShortcuts(cfg)
	case "linux", "android":
		if isTermux() {
			return createTermuxShortcuts(in, cfg)
		}
		return createLinuxDesktopFiles(cfg)
	default:
		return fmt.Errorf("ярлыки для платформы %s пока не поддержаны", runtime.GOOS)
	}
}

func createWindowsShortcuts(cfg config.Config) error {
	desktop := filepath.Join(os.Getenv("USERPROFILE"), "Desktop")
	if _, err := os.Stat(desktop); err != nil {
		return fmt.Errorf("не найдена папка рабочего стола: %s", desktop)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	for _, target := range shortcutTargets(cfg) {
		name := filepath.Join(desktop, "TunnelCtl - "+target+".lnk")
		ps := fmt.Sprintf(`$W=New-Object -ComObject WScript.Shell; $S=$W.CreateShortcut(%q); $S.TargetPath=%q; $S.Arguments=%q; $S.WorkingDirectory=%q; $S.Save()`, name, exe, "connect "+target, filepath.Dir(exe))
		fmt.Println("Создаю ярлык Windows:", name)
		fmt.Println("Команда:")
		fmt.Println("  powershell -NoProfile -ExecutionPolicy Bypass -Command", ps)
		if err := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", ps).Run(); err != nil {
			return err
		}
	}
	return nil
}

func createLinuxDesktopFiles(cfg config.Config) error {
	desktop := linuxDesktopDir()
	if desktop == "" {
		return fmt.Errorf("не найдена папка рабочего стола")
	}
	if err := os.MkdirAll(desktop, 0o755); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	for _, target := range shortcutTargets(cfg) {
		path := filepath.Join(desktop, "tunnelctl-"+safeFile(target)+".desktop")
		body := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=TunnelCtl %s
Comment=Запуск SSH SOCKS-туннеля %s
Exec=%s connect %s
Terminal=true
Categories=Network;
`, target, target, exe, target)
		fmt.Println("Создаю .desktop файл:", path)
		fmt.Println("Команда:")
		fmt.Printf("  cat > %q <<'EOF'\n%sEOF\n", path, body)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func createTermuxShortcuts(in *bufio.Reader, cfg config.Config) error {
	home, _ := os.UserHomeDir()
	shortDir := filepath.Join(home, ".shortcuts")
	if err := os.MkdirAll(shortDir, 0o700); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	for _, target := range shortcutTargets(cfg) {
		path := filepath.Join(shortDir, "tunnelctl-"+safeFile(target)+".sh")
		body := fmt.Sprintf("#!/data/data/com.termux/files/usr/bin/bash\n%s connect %s\n", shellQuote(exe), shellQuote(target))
		fmt.Println("Создаю скрипт для Termux:Widget:", path)
		fmt.Println("Команда:")
		fmt.Printf("  cat > %q <<'EOF'\n%sEOF\n  chmod +x %q\n", path, body, path)
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			return err
		}
	}
	if ask(in, "Создать скрипт автозапуска Termux:Boot для connect auto?", false) {
		bootDir := filepath.Join(home, ".termux", "boot")
		if err := os.MkdirAll(bootDir, 0o700); err != nil {
			return err
		}
		path := filepath.Join(bootDir, "tunnelctl-auto.sh")
		body := fmt.Sprintf("#!/data/data/com.termux/files/usr/bin/bash\nif command -v termux-wake-lock >/dev/null 2>&1; then termux-wake-lock; fi\n%s connect auto --watch >> %s 2>&1 &\n", shellQuote(exe), shellQuote(filepath.Join(home, "tunnelctl-boot.log")))
		fmt.Println("Создаю скрипт Termux:Boot:", path)
		fmt.Println("Команда:")
		fmt.Printf("  mkdir -p %q && cat > %q <<'EOF'\n%sEOF\n  chmod +x %q\n", bootDir, path, body, path)
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			return err
		}
	}
	fmt.Println("Для ярлыков на домашнем экране установи Termux:Widget и добавь виджет с папкой .shortcuts.")
	fmt.Println("Команда установки Termux-пакета API при необходимости:")
	fmt.Println("  pkg install termux-api")
	return nil
}

func shortcutTargets(cfg config.Config) []string {
	var out []string
	for _, g := range cfg.Groups {
		if g.Alias != "" {
			out = append(out, g.Alias)
		} else if g.Name != "" {
			out = append(out, g.Name)
		}
	}
	for _, p := range cfg.Profiles {
		if p.Alias != "" {
			out = append(out, p.Alias)
		} else if p.Name != "" {
			out = append(out, p.Name)
		}
	}
	return out
}

func linuxDesktopDir() string {
	if out, err := exec.Command("xdg-user-dir", "DESKTOP").Output(); err == nil {
		p := strings.TrimSpace(string(out))
		if p != "" && p != os.Getenv("HOME") {
			return p
		}
	}
	home, _ := os.UserHomeDir()
	candidates := []string{filepath.Join(home, "Desktop"), filepath.Join(home, "Рабочий стол")}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return candidates[0]
}

func isTermux() bool {
	return strings.Contains(os.Getenv("PREFIX"), "com.termux") || fileExists("/data/data/com.termux/files/usr/bin/pkg")
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func safeFile(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "/", "-")
	return s
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func ask(in *bufio.Reader, q string, def bool) bool {
	suffix := "[Y/n]"
	if !def {
		suffix = "[y/N]"
	}
	fmt.Printf("%s %s ", q, suffix)
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return def
	}
	return line == "y" || line == "yes" || line == "д" || line == "да"
}
