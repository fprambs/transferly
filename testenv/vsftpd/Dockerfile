# vsftpd test servers: plain FTP, explicit FTPS and implicit FTPS from one image.
# Test use only: fixed credentials and a self-signed certificate. The plain server also lets anonymous users read /srv/anon.
FROM debian:bookworm-slim

# Package downloads use HTTPS so a middlebox cannot alter them; the slim image ships no CA bundle.
COPY --from=golang:1.27-bookworm /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
RUN sed -i 's|http://|https://|g' /etc/apt/sources.list.d/debian.sources

RUN printf 'Acquire::Retries "5";\n' > /etc/apt/apt.conf.d/80-retries \
    && apt-get update \
    && apt-get install -y --no-install-recommends --fix-missing vsftpd openssl ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --home-dir /srv/ftp --shell /bin/sh transferly \
    && echo 'transferly:transferly' | chpasswd \
    && mkdir -p /var/run/vsftpd/empty /srv/ftp/upload \
    && chown -R transferly /srv/ftp \
    && mkdir -p /srv/anon/pub \
    && echo 'public file of the anonymous test server' > /srv/anon/pub/readme.txt \
    && chmod -R a+rX,go-w /srv/anon

RUN openssl req -x509 -nodes -newkey rsa:2048 -days 3650 -subj "/CN=transferly-test" \
    -keyout /etc/ssl/private/vsftpd.key -out /etc/ssl/certs/vsftpd.pem

COPY plain.conf explicit-ftps.conf implicit-ftps.conf /etc/vsftpd/
COPY --chmod=755 entrypoint.sh /usr/local/bin/entrypoint.sh

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["/etc/vsftpd/plain.conf"]
