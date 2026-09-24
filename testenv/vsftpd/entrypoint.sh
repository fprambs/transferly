#!/bin/sh
set -eu

config=$(mktemp)
cp "$1" "$config"
printf 'pasv_address=%s\n' "$(hostname -i | awk '{print $1}')" >> "$config"

touch /var/log/vsftpd.log
tail -F /var/log/vsftpd.log &
exec vsftpd "$config"
