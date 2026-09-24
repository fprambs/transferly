COMPOSE := docker compose -f compose.dev.yaml
TOOLS := $(COMPOSE) run --rm -T tools

.PHONY: up down testenv logs shell test lint hooks

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) --profile testenv down

testenv:
	$(COMPOSE) --profile testenv up -d --build

logs:
	$(COMPOSE) logs -f

shell:
	$(COMPOSE) run --rm tools bash

test:
	$(TOOLS) sh -c './scripts/coverage-go.sh && cd frontend && pnpm test:coverage'

lint:
	$(TOOLS) sh -c 'cd backend && golangci-lint run ./... && cd ../frontend && pnpm lint && pnpm typecheck'

hooks:
	git config core.hooksPath .githooks
.PHONY: gen

gen:
	$(TOOLS) ./scripts/generate.sh
