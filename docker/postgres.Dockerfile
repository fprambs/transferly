# Rebuild the official entrypoint's privilege helper with a patched Go toolchain.
FROM golang:1.27.4-trixie@sha256:430985542100b871e108ccc00e8dcc5eef08ca14ee5825e98ebf563d8ffec4ee AS build
RUN CGO_ENABLED=0 GOTOOLCHAIN=local GOBIN=/out go install -trimpath github.com/tianon/gosu@6456aaa

FROM postgres:17-bookworm@sha256:1eff563f6c868b9b433566e9f3cf6e5c79fbd3b95bf70ec7277d376cdbfc50d7
COPY --from=build /out/gosu /usr/local/bin/gosu

# Bookworm's security repository ships the PAM fix before the official image.
RUN printf '%s\n' 'deb [signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] http://deb.debian.org/debian-security bookworm-security main' > /tmp/bookworm-security.list \
    && apt-get -o Dir::Etc::sourcelist=/tmp/bookworm-security.list -o Dir::Etc::sourceparts=- update \
    && apt-get -o Dir::Etc::sourcelist=/tmp/bookworm-security.list -o Dir::Etc::sourceparts=- \
        install -y --no-install-recommends --only-upgrade \
        libpam-modules libpam-modules-bin libpam-runtime libpam0g \
    && dpkg --compare-versions "$(dpkg-query -W -f='${Version}' libpam0g)" ge '1.5.2-6+deb12u3' \
    && rm -rf /var/lib/apt/lists/* /tmp/bookworm-security.list
