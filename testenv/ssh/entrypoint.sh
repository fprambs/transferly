#!/bin/sh
# Copies the samples mounted at /samples into the home of transferly when there are any, and runs sshd.
set -eu
if [ -d /samples ] && [ ! -e /home/transferly/preview-samples ]; then
  cp -R /samples /home/transferly/preview-samples
  chown -R transferly:transferly /home/transferly/preview-samples
fi
exec /usr/sbin/sshd -D -e
