#!/usr/bin/env bash
set -euo pipefail

# Стартовый скрипт tunnelctl для Termux, Ubuntu и Debian.
# Задача скрипта — довести пользователя до собранного бинарника и запустить мастер настройки.

APP="tunnelctl"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST_DIR="$ROOT_DIR/dist"
BIN="$DIST_DIR/$APP"
GO_FALLBACK_VERSION="1.26.2"

say() { printf '%s\n' "$*"; }
ask_yes_no() {
  local prompt="$1"
  local default="${2:-Y}"
  local answer
  if [[ "$default" == "Y" ]]; then
    read -r -p "$prompt [Y/n]: " answer || true
    [[ -z "$answer" || "$answer" =~ ^[YyДд]$ || "$answer" =~ ^[Дд][Аа]$ || "$answer" =~ ^[Yy][Ee][Ss]$ ]]
  else
    read -r -p "$prompt [y/N]: " answer || true
    [[ "$answer" =~ ^[YyДд]$ || "$answer" =~ ^[Дд][Аа]$ || "$answer" =~ ^[Yy][Ee][Ss]$ ]]
  fi
}

is_termux() {
  [[ -n "${PREFIX:-}" && "$PREFIX" == *"com.termux"* ]] || [[ -d /data/data/com.termux/files/usr ]]
}

has_working_sudo() {
  command -v sudo >/dev/null 2>&1 || return 1
  say "Проверяю, доступен ли sudo..."
  if sudo -n true >/dev/null 2>&1; then
    return 0
  fi
  if sudo -v >/dev/null 2>&1; then
    return 0
  fi
  return 1
}

install_go_to_profile() {
  local os arch tar_name url opt_dir bin_dir profile_file
  os="linux"
  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    armv7l|armv8l) arch="armv6l" ;;
    *) say "Не знаю, какой архив Go скачать для архитектуры: $(uname -m)"; return 1 ;;
  esac
  tar_name="go${GO_FALLBACK_VERSION}.${os}-${arch}.tar.gz"
  url="https://go.dev/dl/${tar_name}"
  opt_dir="$HOME/.local/opt"
  bin_dir="$HOME/.local/bin"
  profile_file="$HOME/.profile"

  say "sudo недоступен. Установлю Go в профиль пользователя."
  say "Будет выполнена команда:"
  say "  mkdir -p '$opt_dir' '$bin_dir' && curl -fL '$url' -o '/tmp/$tar_name' && tar -C '$opt_dir' -xzf '/tmp/$tar_name' && ln -sf '$opt_dir/go/bin/go' '$bin_dir/go'"
  if ! ask_yes_no "Продолжить установку Go в профиль пользователя?" "Y"; then
    say "Установка отменена. Установи Go вручную и снова запусти ./start.sh"
    exit 1
  fi
  mkdir -p "$opt_dir" "$bin_dir"
  if command -v curl >/dev/null 2>&1; then
    curl -fL "$url" -o "/tmp/$tar_name"
  elif command -v wget >/dev/null 2>&1; then
    wget -O "/tmp/$tar_name" "$url"
  else
    say "Нет curl/wget. Сначала установи один из них."
    return 1
  fi
  rm -rf "$opt_dir/go"
  tar -C "$opt_dir" -xzf "/tmp/$tar_name"
  ln -sf "$opt_dir/go/bin/go" "$bin_dir/go"
  export PATH="$bin_dir:$opt_dir/go/bin:$PATH"
  if ! grep -q '.local/opt/go/bin' "$profile_file" 2>/dev/null; then
    {
      echo ''
      echo '# Go для tunnelctl'
      echo 'export PATH="$HOME/.local/bin:$HOME/.local/opt/go/bin:$PATH"'
    } >> "$profile_file"
    say "Добавил Go в PATH через $profile_file"
  fi
}

install_go_if_needed() {
  if command -v go >/dev/null 2>&1; then
    say "Go найден: $(go version)"
    return 0
  fi

  say "Go не найден. Он нужен для сборки tunnelctl."
  if is_termux; then
    say "Команда установки для Termux:"
    say "  pkg update && pkg install -y golang git curl"
    if ask_yes_no "Выполнить установку через pkg?" "Y"; then
      pkg update
      pkg install -y golang git curl
    else
      say "Установка отменена. Установи Go вручную: pkg install golang"
      exit 1
    fi
  elif [[ -f /etc/debian_version ]]; then
    say "Система похожа на Debian/Ubuntu."
    if has_working_sudo; then
      say "Команда установки через apt:"
      say "  sudo apt update && sudo apt install -y golang-go git curl"
      if ask_yes_no "Выполнить установку через sudo apt?" "Y"; then
        sudo apt update
        sudo apt install -y golang-go git curl
      else
        say "Установка отменена. Установи Go вручную или снова запусти ./start.sh"
        exit 1
      fi
    else
      install_go_to_profile
    fi
  else
    say "Неизвестная Linux-система. Попробую установить Go в профиль пользователя."
    install_go_to_profile
  fi
}

build_app() {
  mkdir -p "$DIST_DIR"
  say "Собираю tunnelctl..."
  say "Команда сборки:"
  say "  go build -o '$BIN' ./cmd/tunnelctl"
  (cd "$ROOT_DIR" && go mod download && go build -o "$BIN" ./cmd/tunnelctl)
  say "Готово: $BIN"
}

main() {
  say "Мастер первого запуска tunnelctl"
  say "Папка проекта: $ROOT_DIR"
  install_go_if_needed
  build_app
  say "Запускаю мастер настройки..."
  exec "$BIN" bootstrap
}

main "$@"
