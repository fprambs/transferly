# Apache httpd with mod_dav, the WebDAV server of the test environments (ADR-0026).
FROM httpd:2.4-bookworm@sha256:fbc12199ccad031d8047e9c789d65aceee2d14f99ba90664cd3a3996867a5582
COPY httpd.conf /usr/local/apache2/conf/httpd.conf
COPY entrypoint.sh /entrypoint.sh
RUN htpasswd -bc /usr/local/apache2/conf/dav.passwd transferly transferly && chmod 0755 /entrypoint.sh
EXPOSE 80
ENTRYPOINT ["/entrypoint.sh"]
