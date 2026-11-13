# Apache httpd with mod_dav, the WebDAV server of the test environments (ADR-0026).
FROM httpd:2.4-trixie@sha256:32e9403360ab2f1047523e72f256527fb9aee68b342abf6ad03f0d629157f3d9
COPY httpd.conf /usr/local/apache2/conf/httpd.conf
COPY entrypoint.sh /entrypoint.sh
RUN htpasswd -bc /usr/local/apache2/conf/dav.passwd transferly transferly && chmod 0755 /entrypoint.sh
EXPOSE 80
ENTRYPOINT ["/entrypoint.sh"]
