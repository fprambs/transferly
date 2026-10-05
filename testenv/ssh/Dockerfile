# An OpenSSH server with a shell and the usual tools, for the SSH tools of SFTP sites (ADR-0027): the account transferly, with
# the password transferly, may run commands, unlike the sftp service, which only offers SFTP.
FROM debian:bookworm-slim@sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251
RUN apt-get update \
 && apt-get install -y --no-install-recommends openssh-server zip unzip \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --shell /bin/bash transferly \
 && echo 'transferly:transferly' | chpasswd \
 && mkdir -p /run/sshd && ssh-keygen -A \
 && printf 'PasswordAuthentication yes\nKbdInteractiveAuthentication no\nPermitRootLogin no\nUsePAM no\n' >/etc/ssh/sshd_config.d/transferly.conf
COPY entrypoint.sh /entrypoint.sh
RUN chmod 0755 /entrypoint.sh
EXPOSE 22
ENTRYPOINT ["/entrypoint.sh"]
