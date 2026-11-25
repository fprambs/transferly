COMPOSE := docker compose -f compose.dev.yaml
TOOLS := $(COMPOSE) run --rm -T tools

TEST := docker compose -f compose.test.yaml

# The image that scans the production image; compose.audit.yaml pins the same one.
AUDIT_TRIVY := $(shell sed -n 's/^ *image: \(aquasec\/trivy.*\)/\1/p' compose.audit.yaml)

# The browser tests to run, such as SPECS="specs/local.spec.ts"; every one when empty.
SPECS ?=

.PHONY: up down testenv preview-sftp logs shell test unit e2e e2e-update lint audit hooks

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) --profile testenv down

testenv:
	$(COMPOSE) --profile testenv up -d --build

preview-sftp:
	./scripts/dev-preview-sftp.sh

seed:
	./scripts/seed-testenv.sh

logs:
	$(COMPOSE) logs -f

shell:
	$(COMPOSE) run --rm tools bash

test: unit e2e

unit:
	$(TEST) run --rm unit

e2e:
	sh ./scripts/test-browser.sh $(SPECS)

e2e-update:
	sh ./scripts/test-browser.sh --update-snapshots

lint:
	$(TOOLS) sh -c 'cd backend && golangci-lint run ./... && cd ../frontend && pnpm lint && pnpm typecheck'

# Looks for known vulnerabilities in the dependencies, the production image and the Git history; see docs/security/README.md.
audit:
	$(TOOLS) sh -c 'cd backend && govulncheck ./... && cd ../frontend && pnpm audit'
	mkdir -p /tmp/transferly-audit
	docker compose -f compose.audit.yaml run --rm -T secrets
	docker compose build app
	docker run --rm -v /var/run/docker.sock:/var/run/docker.sock:ro $(AUDIT_TRIVY) image --scanners vuln --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1 transferly:local

hooks:
	git config core.hooksPath .githooks
.PHONY: gen

gen:
	./scripts/generate-tui-client.sh
	$(TOOLS) ./scripts/generate.sh

# Draws the screens of the design of the terminal client with ratatui into docs/design/terminal-client-mockups.md, and photographs
# them in their theme colors into docs/design/screens.
.PHONY: tui-mockups
tui-mockups:
	mkdir -p tui/mockups/pages
	docker run --rm -v $(CURDIR):/repo -v transferly-tui-cargo:/usr/local/cargo/registry -v transferly-tui-target:/target -e CARGO_TARGET_DIR=/target \
		-w /repo/tui/mockups rust:1.100-bookworm sh -c 'cargo run -q --release --locked -- /repo/docs/design/terminal-client-mockups.md /repo/tui/mockups/pages && chown -R $(shell id -u):$(shell id -g) /repo/docs/design/terminal-client-mockups.md /repo/tui/mockups/pages Cargo.lock'
	docker run --rm -v $(CURDIR)/tui/mockups/pages:/out debian:bookworm-slim sh -c 'apt-get -qq update >/dev/null && apt-get -qq install -y fonts-dejavu-core >/dev/null \
		&& cp /usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf /usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf /out/ && chown $(shell id -u):$(shell id -g) /out/*.ttf'
	docker run --rm -u $(shell id -u):$(shell id -g) -e HOME=/tmp -v $(CURDIR):/repo -w /repo/tui/mockups \
		mcr.microsoft.com/playwright:v1.64.1-noble@sha256:2f05f8a734509a7b3f26a912639488ee08f58a7b292d14d62271bcff9038768f node screenshot.cjs pages /repo/docs/design/screens

# The terminal client (tui/), in the Rust image of compose.dev.yaml; see docs/guides/rust-tooling.md.
RUST := $(COMPOSE) run --rm --no-deps -T rust
# Code left out of coverage: the generated client and the entry point, as generated code and cmd/ are in Go.
TUI_COVERAGE_IGNORE := '(tui/client/|tui/src/main\.rs)'

.PHONY: tui-lint tui-test tui-cov tui-deny
tui-lint:
	$(RUST) sh -c 'cargo fmt -p transferly-tui -- --check && cargo clippy -p transferly-tui --all-targets --all-features -- -D warnings'

tui-test:
	$(RUST) sh -c 'cargo nextest run -p transferly-tui && cargo test -p transferly-tui --doc'

tui-cov:
	$(RUST) cargo llvm-cov nextest -p transferly-tui --ignore-filename-regex $(TUI_COVERAGE_IGNORE) --fail-under-lines 100 --fail-under-regions 100 --fail-under-functions 100

tui-deny:
	$(RUST) cargo deny check
