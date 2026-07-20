#!/bin/sh
set -eu

if [ ! -s /run/keys/id_ed25519.pub ]; then
    echo "Не найден тестовый открытый SSH-ключ" >&2
    exit 1
fi

mkdir -p /home/tunnel/.ssh /run/sshd
cp /run/keys/id_ed25519.pub /home/tunnel/.ssh/authorized_keys
chown -R tunnel:tunnel /home/tunnel/.ssh
chmod 0700 /home/tunnel/.ssh
chmod 0600 /home/tunnel/.ssh/authorized_keys
ssh-keygen -A

exec /usr/sbin/sshd -D -e -f /etc/ssh/sshd_config
