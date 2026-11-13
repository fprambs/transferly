# An OpenSSH server with a shell and the usual tools, for the SSH tools of SFTP sites (ADR-0027): the account transferly, with
# the password transferly, may run commands, unlike the sftp service, which only offers SFTP, and may use sudo with its password;
# deployer, with the password deployer, may use sudo without one. /root/only-root.txt can only be read as root.
FROM debian:trixie-slim@sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2
# Package downloads use HTTPS so a middlebox cannot alter them; the slim image ships no CA bundle.
COPY --from=golang:1.27.4-trixie /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
RUN sed -i 's|http://|https://|g' /etc/apt/sources.list.d/debian.sources
RUN printf 'Acquire::Retries "5";\nAcquire::https::CaInfo "/etc/ssl/certs/ca-certificates.crt";\n' > /etc/apt/apt.conf.d/80-retries \
 && apt-get update \
 && apt-get install -y --no-install-recommends openssh-server zip unzip sudo \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --shell /bin/bash transferly \
 && echo 'transferly:transferly' | chpasswd \
 && useradd --create-home --shell /bin/bash deployer \
 && echo 'deployer:deployer' | chpasswd \
 && printf 'transferly ALL=(ALL:ALL) ALL\ndeployer ALL=(ALL:ALL) NOPASSWD: ALL\n' >/etc/sudoers.d/transferly \
 && chmod 0440 /etc/sudoers.d/transferly \
 && echo 'Only root reads this.' >/root/only-root.txt \
 && chmod 0600 /root/only-root.txt \
 && mkdir -p /run/sshd && ssh-keygen -A \
 && printf 'PasswordAuthentication yes\nKbdInteractiveAuthentication no\nPermitRootLogin no\nUsePAM no\n' >/etc/ssh/sshd_config.d/transferly.conf
COPY entrypoint.sh /entrypoint.sh
RUN chmod 0755 /entrypoint.sh
EXPOSE 22
ENTRYPOINT ["/entrypoint.sh"]
