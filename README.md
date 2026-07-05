# tunnelctl

CLI-утилита для управления локальным SOCKS5-прокси поверх SSH-туннеля.

Целевые системы: Termux/Android, Windows, Ubuntu, Debian.

## Состояние проекта

Это первый MVP. Он использует системный `ssh` как управляемый дочерний процесс, а не встроенный SSH-клиент. `tunnelctl` запускает `ssh` без `-f`, проверяет реальную работоспособность SOCKS через health-check и перезапускает процесс при зависании.

## Быстрый старт

### Termux / Ubuntu / Debian

```bash
./start.sh
```

### Windows

```bat
start.cmd
```

Первый запуск проверит наличие Go, при необходимости предложит установку, соберёт программу, запустит мастер настройки, предложит импортировать старые `ssh -D` команды из истории и создать ярлыки.

## Что умеет

- интерактивный выбор туннеля из списка;
- прямой запуск по имени или алиасу;
- импорт старых `ssh -D` команд из истории shell/PowerShell;
- поддержка команд вида `ssh -i ~/.ssh/id_ed25519 -v -D 1080 -N user@host`;
- интеллектуальная генерация имени профиля и алиаса;
- конфиг в стандартном месте ОС;
- проверка живости SOCKS через реальный HTTP/HTTPS-запрос;
- автоматический перезапуск при сбое;
- failover-группа `auto`;
- создание ярлыков Windows/Linux/Termux.

## Основные команды

```bash
tunnelctl
```

Показать меню выбора профиля.

```bash
tunnelctl connect yandex
```

Подключиться к профилю или алиасу.

```bash
tunnelctl connect auto
```

Подключиться к failover-группе.

```bash
tunnelctl bootstrap
```

Повторно запустить мастер настройки.

```bash
tunnelctl doctor
```

Проверить окружение и конфиг.

## Где лежит конфиг

- Linux/Termux: `$XDG_CONFIG_HOME/tunnelctl/tunnelctl.json` или `~/.config/tunnelctl/tunnelctl.json`
- Windows: `%APPDATA%\tunnelctl\tunnelctl.json`

Логи:

- Linux/Termux: `$XDG_STATE_HOME/tunnelctl/tunnelctl.log` или `~/.local/state/tunnelctl/tunnelctl.log`
- Windows: `%LOCALAPPDATA%\tunnelctl\tunnelctl.log`

## Установка зависимостей

Скрипты первого запуска показывают конкретную команду перед установкой.

Termux:

```bash
pkg update && pkg install -y golang git curl
```

Ubuntu/Debian при доступном sudo:

```bash
sudo apt update && sudo apt install -y golang-go git curl
```

Windows через winget:

```powershell
winget install --id GoLang.Go -e
```

Windows через Chocolatey:

```powershell
choco install golang -y
```

Если `sudo` недоступен на Linux, `start.sh` предлагает установить Go в профиль пользователя: `~/.local/opt/go` и `~/.local/bin/go`.
