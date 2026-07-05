package bootstrap

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"

	"tunnelctl/internal/config"
	"tunnelctl/internal/historyscan"
	"tunnelctl/internal/paths"
	"tunnelctl/internal/shortcut"
)

// Run запускает интерактивный мастер настройки.
func Run(configPath string) error {
	in := bufio.NewReader(os.Stdin)
	fmt.Println("Мастер настройки tunnelctl")
	fmt.Println("Платформа:", runtime.GOOS, runtime.GOARCH)
	fmt.Println("Конфиг:", effectiveConfigPath(configPath))

	cfg, created, err := config.Ensure(configPath)
	if err != nil {
		return fmt.Errorf("не удалось подготовить конфиг: %w", err)
	}
	if created {
		fmt.Println("Создан новый конфиг.")
	} else {
		fmt.Println("Найден существующий конфиг.")
	}

	if ask(in, "Просканировать историю команд и импортировать ssh -D туннели?", true) {
		if err := importHistory(in, &cfg, configPath); err != nil {
			fmt.Println("Предупреждение: импорт истории завершился с ошибкой:", err)
		}
	}

	if len(cfg.Profiles) > 1 && len(cfg.Groups) == 0 {
		if ask(in, "Создать failover-группу auto из найденных профилей?", true) {
			profiles := make([]string, 0, len(cfg.Profiles))
			for _, p := range cfg.Profiles {
				profiles = append(profiles, p.Name)
			}
			cfg.Groups = append(cfg.Groups, config.Group{Name: "auto", Alias: "auto", Strategy: "failover", Profiles: profiles})
			_ = config.Save(configPath, cfg)
			fmt.Println("Группа auto создана.")
		}
	}

	if ask(in, "Создать ярлыки/скрипты запуска для доступной платформы?", true) {
		if err := shortcut.CreateInteractive(in, cfg); err != nil {
			fmt.Println("Предупреждение: ярлыки не созданы:", err)
		}
	}

	fmt.Println()
	fmt.Println("Готово. Основные команды:")
	fmt.Println("  tunnelctl")
	fmt.Println("  tunnelctl connect <алиас-или-имя>")
	fmt.Println("  tunnelctl connect auto")
	fmt.Println("  tunnelctl doctor")
	return nil
}

func effectiveConfigPath(p string) string {
	if p != "" {
		return p
	}
	return paths.ConfigPath()
}

func importHistory(in *bufio.Reader, cfg *config.Config, configPath string) error {
	cands, err := historyscan.Scan(*cfg)
	if err != nil {
		return err
	}
	if len(cands) == 0 {
		fmt.Println("Подходящие команды ssh -D в истории не найдены.")
		return nil
	}
	fmt.Printf("Найдено кандидатов: %d\n", len(cands))
	for i, c := range cands {
		fmt.Println()
		fmt.Printf("[%d] %s\n", i+1, c.Command)
		fmt.Printf("    Источник: %s:%d, повторов: %d\n", c.Source, c.Line, c.Seen)
		fmt.Printf("    Пользователь: %s\n", c.Profile.User)
		fmt.Printf("    Сервер:       %s:%d\n", c.Profile.Host, c.Profile.Port)
		fmt.Printf("    Ключ:         %s\n", emptyAsDash(c.Profile.Key))
		fmt.Printf("    Локально:     %s\n", c.Profile.Listen)
		fmt.Printf("    Имя профиля:  %s\n", c.Profile.Name)
		fmt.Printf("    Алиас:        %s\n", c.Profile.Alias)
		if !ask(in, "Добавить этот профиль?", true) {
			continue
		}
		p := c.Profile
		if ask(in, "Отредактировать имя/алиас/порт перед добавлением?", false) {
			p.Name = askString(in, "Имя профиля", p.Name)
			p.Alias = askString(in, "Алиас", p.Alias)
			p.Listen = askString(in, "Локальный адрес", p.Listen)
			p.Key = askString(in, "Ключ", p.Key)
		}
		cfg.Profiles = append(cfg.Profiles, p)
		if err := config.Save(configPath, *cfg); err != nil {
			return err
		}
		fmt.Println("Профиль добавлен.")
	}
	return nil
}

func emptyAsDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

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

func askString(in *bufio.Reader, q, def string) string {
	fmt.Printf("%s [%s]: ", q, def)
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}
