#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/../../.." && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/compose.yml"
TEST_TMP="$(mktemp -d)"
export TUNNELCTL_TEST_TMP="$TEST_TMP"
export COMPOSE_PROJECT_NAME="tunnelctl-integration-$$"
export HOME="$TEST_TMP/home"
export XDG_CONFIG_HOME="$TEST_TMP/config"
export XDG_STATE_HOME="$TEST_TMP/state"
export XDG_RUNTIME_DIR="$TEST_TMP/runtime"

BIN="$TEST_TMP/tunnelctl"
CONFIG="$TEST_TMP/tunnelctl.json"
TUNNEL_LOG="$TEST_TMP/tunnelctl.stdout.log"
TUNNEL_PID=""

compose() {
    docker compose -f "$COMPOSE_FILE" "$@"
}

cleanup() {
    local status=$?
    set +e

    if [[ -n "$TUNNEL_PID" ]] && kill -0 "$TUNNEL_PID" 2>/dev/null; then
        "$BIN" stop >/dev/null 2>&1 || true
        sleep 1
        kill "$TUNNEL_PID" >/dev/null 2>&1 || true
    fi

    if (( status != 0 )); then
        echo "--- tunnelctl stdout/stderr ---" >&2
        cat "$TUNNEL_LOG" >&2 2>/dev/null || true
        echo "--- docker compose logs ---" >&2
        compose logs --no-color >&2 2>/dev/null || true
    fi

    compose down --volumes --remove-orphans >/dev/null 2>&1 || true
    rm -rf "$TEST_TMP"
    return "$status"
}
trap cleanup EXIT

fail() {
    echo "Ошибка интеграционного теста: $*" >&2
    return 1
}

wait_for_ssh() {
    local port=$1
    local output=$2
    local attempt
    for attempt in $(seq 1 40); do
        if ssh-keyscan -p "$port" 127.0.0.1 >"$output" 2>/dev/null && [[ -s "$output" ]]; then
            return 0
        fi
        sleep 0.5
    done
    fail "SSH на порту $port не стал доступен"
}

wait_for_profile() {
    local expected=$1
    local attempt output
    for attempt in $(seq 1 80); do
        if [[ -n "$TUNNEL_PID" ]] && ! kill -0 "$TUNNEL_PID" 2>/dev/null; then
            fail "управляющий процесс tunnelctl завершился раньше времени"
        fi
        output="$("$BIN" status --config "$CONFIG" 2>&1 || true)"
        if grep -Fq "Активный профиль: $expected" <<<"$output" \
            && grep -Fq "Состояние: работает" <<<"$output"; then
            return 0
        fi
        sleep 0.5
    done
    echo "$output" >&2
    fail "профиль $expected не стал активным"
}

wait_for_target() {
    local attempt body
    for attempt in $(seq 1 40); do
        body="$(compose exec -T ssh-one wget -qO- http://target:8080/health 2>/dev/null || true)"
        if [[ "$body" == "tunnelctl integration ok" ]]; then
            return 0
        fi
        sleep 0.5
    done
    fail "тестовый HTTP-ресурс не стал доступен из сети SSH-серверов"
}

assert_socks() {
    local body
    body="$(curl --fail --silent --show-error \
        --socks5-hostname 127.0.0.1:1080 \
        --max-time 5 \
        http://target:8080/health)"
    [[ "$body" == "tunnelctl integration ok" ]] || fail "неожиданный ответ через SOCKS: $body"
}

for required_command in docker go ssh-keygen ssh-keyscan curl; do
    command -v "$required_command" >/dev/null 2>&1 \
        || fail "не найдена обязательная команда: $required_command"
done
docker compose version >/dev/null

cd "$REPO_ROOT"
mkdir -p "$HOME/.ssh" "$XDG_CONFIG_HOME" "$XDG_STATE_HOME" "$XDG_RUNTIME_DIR"
chmod 0700 "$HOME/.ssh" "$XDG_RUNTIME_DIR"

ssh-keygen -q -t ed25519 -N '' -f "$TEST_TMP/id_ed25519"
go build -o "$BIN" ./cmd/tunnelctl

compose up -d --build
wait_for_ssh 22221 "$TEST_TMP/known_hosts.one"
wait_for_ssh 22222 "$TEST_TMP/known_hosts.two"
wait_for_target
cat "$TEST_TMP/known_hosts.one" "$TEST_TMP/known_hosts.two" > "$HOME/.ssh/known_hosts"
chmod 0600 "$HOME/.ssh/known_hosts"

cat > "$CONFIG" <<EOF_CONFIG
{
  "defaults": {
    "listen": "127.0.0.1:1080",
    "health_url": "http://target:8080/health",
    "health_interval_sec": 1,
    "health_timeout_sec": 2,
    "connect_timeout_sec": 3,
    "reconnect": {
      "enabled": true,
      "min_delay_sec": 1,
      "max_delay_sec": 2
    },
    "port_conflict": "error",
    "allow_listen_all_hosts": false
  },
  "profiles": [
    {
      "name": "ssh-one",
      "user": "tunnel",
      "host": "127.0.0.1",
      "port": 22221,
      "key": "$TEST_TMP/id_ed25519"
    },
    {
      "name": "ssh-two",
      "user": "tunnel",
      "host": "127.0.0.1",
      "port": 22222,
      "key": "$TEST_TMP/id_ed25519"
    }
  ],
  "groups": [
    {
      "name": "auto",
      "strategy": "ordered",
      "profiles": ["ssh-one", "ssh-two"]
    }
  ]
}
EOF_CONFIG
chmod 0600 "$CONFIG"

"$BIN" connect auto --watch --config "$CONFIG" >"$TUNNEL_LOG" 2>&1 &
TUNNEL_PID=$!

wait_for_profile ssh-one
assert_socks

echo "Проверяется автоматический failover ssh-one -> ssh-two"
compose stop -t 0 ssh-one
wait_for_profile ssh-two
assert_socks

echo "Проверяется switch next после восстановления ssh-one"
compose start ssh-one
wait_for_ssh 22221 "$TEST_TMP/known_hosts.restarted"
"$BIN" switch next
wait_for_profile ssh-one
assert_socks

echo "Проверяется явный switch ssh-two"
"$BIN" switch ssh-two
wait_for_profile ssh-two
assert_socks

echo "Проверяется корректный stop"
"$BIN" stop
wait "$TUNNEL_PID"
TUNNEL_PID=""

[[ ! -e "$XDG_STATE_HOME/tunnelctl/tunnelctl.state.json" ]] \
    || fail "после stop остался state-файл"
[[ ! -e "$XDG_RUNTIME_DIR/tunnelctl/control.sock" ]] \
    || fail "после stop остался управляющий сокет"
if (echo > /dev/tcp/127.0.0.1/1080) >/dev/null 2>&1; then
    fail "после stop SOCKS-порт остался открыт"
fi

status_output="$("$BIN" status --config "$CONFIG" 2>&1)"
grep -Fq "Управляемый tunnelctl сейчас не запущен." <<<"$status_output" \
    || fail "status после stop не подтвердил остановку"

echo "Docker-интеграционный тест tunnelctl успешно завершён"
