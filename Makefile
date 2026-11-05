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
	$(TOOLS) ./scripts/generate.sh
