COMPOSE := docker compose -f compose.dev.yaml
TOOLS := $(COMPOSE) run --rm -T tools

TEST := docker compose -f compose.test.yaml

.PHONY: up down testenv preview-sftp logs shell test unit e2e e2e-update lint hooks

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
	$(TEST) --profile ftp --profile s3 --profile google --profile azure --profile webdav --profile ssh up -d --build --force-recreate --wait postgres transferly sftp ftp ftps ftps-implicit s3 fakegoogle azurite fakeazure webdav ssh strict-sftp
	$(TEST) run --rm e2e

e2e-update:
	$(TEST) --profile ftp --profile s3 --profile google --profile azure --profile webdav --profile ssh up -d --build --force-recreate --wait postgres transferly sftp ftp ftps ftps-implicit s3 fakegoogle azurite fakeazure webdav ssh strict-sftp
	$(TEST) run --rm e2e node_modules/.bin/playwright test --update-snapshots

lint:
	$(TOOLS) sh -c 'cd backend && golangci-lint run ./... && cd ../frontend && pnpm lint && pnpm typecheck'

hooks:
	git config core.hooksPath .githooks
.PHONY: gen

gen:
	$(TOOLS) ./scripts/generate.sh
