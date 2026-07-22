package bootstrap

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"tunnelctl/internal/config"
	"tunnelctl/internal/historyscan"
	"tunnelctl/internal/paths"
	"tunnelctl/internal/shortcut"
)

const maxImportCandidates = 10

type dependencies struct {
	ensure    func(string) (config.Config, bool, error)
	save      func(string, config.Config) error
	scan      func(config.Config) ([]historyscan.Candidate, error)
	shortcuts func(*bufio.Reader, config.Config) error
	goos      string
}

func defaultDependencies() dependencies {
	return dependencies{
		ensure:    config.Ensure,
		save:      config.Save,
		scan:      historyscan.Scan,
		shortcuts: shortcut.CreateInteractive,
		goos:      runtime.GOOS,
	}
}

// Run запускает интерактивный мастер настройки.
func Run(configPath string) error {
	return run(bufio.NewReader(os.Stdin), os.Stdout, configPath, defaultDependencies())
}

func run(in *bufio.Reader, out io.Writer, configPath string, deps dependencies) error {
	fmt.Fprintln(out, "Мастер настройки tunnelctl")
	fmt.Fprintln(out, "Платформа:", deps.goos, runtime.GOARCH)
	fmt.Fprintln(out, "Конфиг:", effectiveConfigPath(configPath))

	cfg, created, err := deps.ensure(configPath)
	if err != nil {
		return fmt.Errorf("не удалось подготовить конфиг: %w", err)
	}
	if created {
		fmt.Fprintln(out, "Создан новый конфиг.")
	} else {
		fmt.Fprintln(out, "Найден существующий конфиг.")
	}

	if ask(in, out, "Просканировать историю команд и импортировать ssh -D туннели?", true) {
		if _, err := importHistory(in, out, &cfg, configPath, deps.scan, deps.save); err != nil {
			fmt.Fprintln(out, "Предупреждение: импорт истории завершился с ошибкой:", err)
			if len(cfg.Profiles) == 0 && len(cfg.Groups) == 0 {
				printManualSetup(out, effectiveConfigPath(configPath))
			}
		}
	} else if len(cfg.Profiles) == 0 && len(cfg.Groups) == 0 {
		printManualSetup(out, effectiveConfigPath(configPath))
	}

	if len(cfg.Profiles) > 1 && len(cfg.Groups) == 0 {
		if ask(in, out, "Создать failover-группу auto из найденных профилей?", true) {
			profiles := make([]string, 0, len(cfg.Profiles))
			for _, p := range cfg.Profiles {
				profiles = append(profiles, p.Name)
			}
			cfg.Groups = append(cfg.Groups, config.Group{Name: "auto", Alias: "auto", Strategy: "failover", Profiles: profiles})
			if err := deps.save(configPath, cfg); err != nil {
				return fmt.Errorf("не удалось сохранить группу auto: %w", err)
			}
			fmt.Fprintln(out, "Группа auto создана.")
		}
	}

	if len(cfg.Profiles) > 0 || len(cfg.Groups) > 0 {
		if ask(in, out, "Создать ярлыки/скрипты запуска для доступной платформы?", true) {
			if err := deps.shortcuts(in, cfg); err != nil {
				fmt.Fprintln(out, "Предупреждение: ярлыки не созданы:", err)
			}
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Готово. Основные команды:")
	fmt.Fprintln(out, "  tunnelctl")
	fmt.Fprintln(out, "  tunnelctl connect <алиас-или-имя>")
	if len(cfg.Groups) > 0 {
		fmt.Fprintln(out, "  tunnelctl connect auto")
	}
	fmt.Fprintln(out, "  tunnelctl doctor")
	return nil
}

func effectiveConfigPath(p string) string {
	if p != "" {
		return p
	}
	return paths.ConfigPath()
}

type scanFunc func(config.Config) ([]historyscan.Candidate, error)
type saveFunc func(string, config.Config) error

func importHistory(in *bufio.Reader, out io.Writer, cfg *config.Config, configPath string, scan scanFunc, save saveFunc) (int, error) {
	candidates, err := scan(*cfg)
	if err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		fmt.Fprintln(out, "Подходящие команды ssh -D в истории не найдены.")
		printManualSetup(out, effectiveConfigPath(configPath))
		return 0, nil
	}

	shown := candidates
	if len(shown) > maxImportCandidates {
		shown = shown[:maxImportCandidates]
	}
	fmt.Fprintf(out, "Найдено уникальных туннелей: %d. Показано кандидатов: %d.\n", len(candidates), len(shown))
	if len(candidates) > maxImportCandidates {
		fmt.Fprintf(out, "Импорт ограничен первыми %d кандидатами; остальные %d не показаны.\n", maxImportCandidates, len(candidates)-maxImportCandidates)
	}
	for i, candidate := range shown {
		p := candidate.Profile
		fmt.Fprintf(out, "  %d. %s@%s, SOCKS %s, ключ %s, имя %s, алиас %s\n",
			i+1, p.User, p.Address(), p.EffectiveListen(*cfg), emptyAsDash(p.Key), p.Name, emptyAsDash(p.Alias))
	}

	for {
		fmt.Fprint(out, "Номера для импорта через запятую (пусто — не импортировать): ")
		line, readErr := in.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return 0, readErr
		}
		indexes, declined, parseErr := parseSelection(line, len(shown))
		if parseErr != nil {
			fmt.Fprintln(out, "Неверный выбор:", parseErr)
			if errors.Is(readErr, io.EOF) {
				return 0, parseErr
			}
			continue
		}
		if declined {
			fmt.Fprintln(out, "Импорт не выполнен.")
			printManualSetup(out, effectiveConfigPath(configPath))
			return 0, nil
		}

		next := *cfg
		next.Profiles = append([]config.Profile(nil), cfg.Profiles...)
		for _, index := range indexes {
			next.Profiles = append(next.Profiles, shown[index].Profile)
		}
		if err := next.Validate(); err != nil {
			fmt.Fprintln(out, "Выбранные профили нельзя добавить:", err)
			if errors.Is(readErr, io.EOF) {
				return 0, err
			}
			continue
		}
		if err := save(configPath, next); err != nil {
			return 0, err
		}
		*cfg = next

		fmt.Fprintf(out, "Импортировано профилей: %d. Конфиг сохранён: %s\n", len(indexes), effectiveConfigPath(configPath))
		for i, index := range indexes {
			p := shown[index].Profile
			fmt.Fprintf(out, "  %d. %s, алиас %s, %s@%s, SOCKS %s\n",
				i+1, p.Name, emptyAsDash(p.Alias), p.User, p.Address(), p.EffectiveListen(next))
		}
		return len(indexes), nil
	}
}

func parseSelection(value string, max int) ([]int, bool, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "n" || value == "no" || value == "н" || value == "нет" || value == "-" {
		return nil, true, nil
	}
	parts := strings.Split(value, ",")
	seen := make(map[int]bool, len(parts))
	indexes := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false, errors.New("между запятыми должен быть номер")
		}
		number, err := strconv.Atoi(part)
		if err != nil {
			return nil, false, fmt.Errorf("%q не является номером", part)
		}
		if number < 1 || number > max {
			return nil, false, fmt.Errorf("номер %d вне диапазона 1..%d", number, max)
		}
		if seen[number] {
			return nil, false, fmt.Errorf("номер %d указан повторно", number)
		}
		seen[number] = true
		indexes = append(indexes, number-1)
	}
	return indexes, false, nil
}

func printManualSetup(out io.Writer, configPath string) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Настройка вручную:")
	fmt.Fprintln(out, "  Файл:", configPath)
	fmt.Fprintln(out, `  Добавь в profiles профиль вида:`)
	fmt.Fprintln(out, `    {"name":"my-tunnel","alias":"my","user":"user","host":"example.com","port":22,"key":"~/.ssh/id_ed25519","listen":"127.0.0.1:1080"}`)
	fmt.Fprintln(out, "  Проверка: tunnelctl doctor")
	fmt.Fprintln(out, "  Запуск:   tunnelctl connect my-tunnel")
	fmt.Fprintln(out)
}

func emptyAsDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func ask(in *bufio.Reader, out io.Writer, question string, def bool) bool {
	suffix := "[Y/n]"
	if !def {
		suffix = "[y/N]"
	}
	fmt.Fprintf(out, "%s %s ", question, suffix)
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return def
	}
	return line == "y" || line == "yes" || line == "д" || line == "да"
}
