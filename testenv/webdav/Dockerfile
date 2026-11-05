# Apache httpd with mod_dav, the WebDAV server of the test environments (ADR-0026).
FROM httpd:2.4-trixie@sha256:f6a86043744215e4be90cf8804375d90022a711d892c9aa2d7085c089ee2cb54
COPY httpd.conf /usr/local/apache2/conf/httpd.conf
COPY entrypoint.sh /entrypoint.sh
RUN htpasswd -bc /usr/local/apache2/conf/dav.passwd transferly transferly && chmod 0755 /entrypoint.sh
EXPOSE 80
ENTRYPOINT ["/entrypoint.sh"]
