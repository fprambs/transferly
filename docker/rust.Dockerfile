# Rust tools image of the terminal client (tui/): the toolchain, its components and the quality tools of docs/guides/rust-tooling.md.
# Kept apart from tools.Dockerfile so the Go and Node image stays small. The host installs nothing.
ARG RUST_VERSION=1.100

FROM rust:${RUST_VERSION}-bookworm

ENV CARGO_HOME=/cache/cargo \
    CARGO_TARGET_DIR=/cache/target \
    HOME=/cache/home \
    PATH=/usr/local/cargo/bin:/cache/cargo/bin:$PATH

# musl-tools builds the static Linux binaries cargo-dist releases; the components lint, format and measure coverage.
RUN sed -i 's|http://|https://|g' /etc/apt/sources.list.d/debian.sources \
    && printf 'Acquire::Retries "5";\n' > /etc/apt/apt.conf.d/80-retries \
    && apt-get update \
    && apt-get install -y --no-install-recommends musl-tools \
    && rm -rf /var/lib/apt/lists/* \
    && rustup component add clippy rustfmt llvm-tools-preview \
    && rustup target add x86_64-unknown-linux-musl

# Quality tools, pinned by version and installed into the toolchain's own bin, outside the cache volume.
ARG NEXTEST_VERSION=0.9.152
ARG LLVM_COV_VERSION=0.9.2
ARG DENY_VERSION=0.22.1
RUN CARGO_HOME=/usr/local/cargo cargo install --locked cargo-nextest@${NEXTEST_VERSION} cargo-llvm-cov@${LLVM_COV_VERSION} cargo-deny@${DENY_VERSION} \
    && rm -rf /usr/local/cargo/registry /usr/local/cargo/git

# Cache directories writable by any UID; named volumes inherit these permissions.
RUN mkdir -p /cache/cargo /cache/target /cache/home && chmod -R 0777 /cache

WORKDIR /workspace/tui
