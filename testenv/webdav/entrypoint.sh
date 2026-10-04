#!/bin/sh
# Seeds the shares, from the samples mounted at /samples when there are any, and runs Apache in the foreground.
set -eu
mkdir -p /var/dav/files /var/dav/public /var/dav/lock
printf 'Welcome to the public WebDAV share of the Transferly test environment.\n' >/var/dav/public/readme.txt
if [ -d /samples ]; then
  cp -R /samples /var/dav/files/preview-samples
  cp -R /samples /var/dav/public/preview-samples
fi
chown -R daemon:daemon /var/dav
exec httpd -DFOREGROUND
