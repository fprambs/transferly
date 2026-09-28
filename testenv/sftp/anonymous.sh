#!/bin/sh
# Lets the anonymous user of the test SFTP server in with the SSH "none" method: an empty password that sshd accepts
# without asking, which is how OpenSSH grants access with no authentication (RFC 4252, section 5.2).
set -eu
passwd -d anonymous >/dev/null
sed -i '/^PermitEmptyPasswords/d' /etc/ssh/sshd_config
echo 'PermitEmptyPasswords yes' >> /etc/ssh/sshd_config
mkdir -p /home/anonymous/pub
echo 'Welcome, anonymous.' > /home/anonymous/pub/readme.txt
chown -R root:root /home/anonymous/pub
