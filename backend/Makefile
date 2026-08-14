# Load .env if present (developer-local overrides).
# Explicit no-op rule keeps the catch-all "%:" fallback below from being used to
# "build" a missing .env — that would recursively re-invoke $(MAKE), which itself
# re-evaluates this same -include and recurses without end.
.env: ;

-include .env
export

.PHONY: help build build-api build-api-web sync-web-dist generate-well-known build-ws build-llm build-push build-migrate build-voicebridge build-gateway \
       run-api run-ws run-llm run-push run-gateway \
       tidy migrate reset-db flush-redis dev-reset dev-stop dev-up dev-restart \
	       schema-verify clean

DB_HOST ?= 127.0.0.1
DB_PORT ?= 5432
DB_USER ?= aibot
DB_PASSWORD ?= aibot_secret
DB_NAME ?= aibot
MIGRATION_INIT ?= migration/001_init_schema.sql

REDIS_HOST ?= 127.0.0.1
REDIS_PORT ?= 6379
REDIS_DB ?= 0
REDIS_PASSWORD ?=

API_PORT ?= 27180
WS_PORT ?= 27189
LLM_PORT ?= 27182
PUSH_PORT ?= 27183
GATEWAY_PORT ?= 27184
SCHEMA_VERIFY_SCRIPT ?= ../docs/backend/websocket/schemas/validate_examples.sh
SKIP_WEB_BUILD ?= 1

# Build all services
build: build-api build-ws build-llm build-push build-gateway build-migrate build-voicebridge

build-api:
	go build -o bin/api ./cmd/api

sync-web-dist:
	./scripts/sync_web_dist.sh

generate-well-known:
	./scripts/generate_well_known.sh

build-api-web: sync-web-dist
	go build -o bin/api ./cmd/api

build-ws:
	go build -o bin/ws ./cmd/ws

build-llm:
	go build -o bin/llm ./cmd/llm

build-push:
	go build -o bin/push ./cmd/push

build-migrate:
	go build -o bin/migrate ./cmd/migrate

build-voicebridge:
	go build -o bin/voicebridge ./cmd/voicebridge

build-gateway:
	go build -o bin/gateway ./cmd/gateway

# Run services (development)
run-api:
	go run ./cmd/api $(CONFIG)

run-ws:
	go run ./cmd/ws $(CONFIG)

run-llm:
	go run ./cmd/llm $(CONFIG)

run-push:
	go run ./cmd/push $(CONFIG)

run-gateway:
	go run ./cmd/gateway $(CONFIG)

# Dependencies
tidy:
	go mod tidy

# Database migration
migrate:
	@set -e; \
	command -v psql >/dev/null 2>&1 || { echo "psql not found"; exit 127; }; \
	export PGPASSWORD="$(DB_PASSWORD)"; \
	[ -f "$(MIGRATION_INIT)" ] || { echo "missing migration file: $(MIGRATION_INIT)"; exit 1; }; \
	echo "Applying $(MIGRATION_INIT)"; \
	psql -h $(DB_HOST) -p $(DB_PORT) -U $(DB_USER) -d $(DB_NAME) -f $(MIGRATION_INIT)

reset-db:
	@set -e; \
	command -v psql >/dev/null 2>&1 || { echo "psql not found"; exit 127; }; \
	export PGPASSWORD="$(DB_PASSWORD)"; \
	echo "Resetting PostgreSQL schema public on $(DB_HOST):$(DB_PORT)/$(DB_NAME)"; \
	psql -h $(DB_HOST) -p $(DB_PORT) -U $(DB_USER) -d $(DB_NAME) -v ON_ERROR_STOP=1 \
	  -c "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;"; \
	$(MAKE) migrate DB_HOST=$(DB_HOST) DB_PORT=$(DB_PORT) DB_USER=$(DB_USER) DB_PASSWORD=$(DB_PASSWORD) DB_NAME=$(DB_NAME)

flush-redis:
	@set -e; \
	command -v redis-cli >/dev/null 2>&1 || { echo "redis-cli not found"; exit 127; }; \
	echo "Flushing Redis db=$(REDIS_DB) on $(REDIS_HOST):$(REDIS_PORT)"; \
	if [ -n "$(REDIS_PASSWORD)" ]; then \
		redis-cli -h $(REDIS_HOST) -p $(REDIS_PORT) -a "$(REDIS_PASSWORD)" -n $(REDIS_DB) FLUSHDB; \
	else \
		redis-cli -h $(REDIS_HOST) -p $(REDIS_PORT) -n $(REDIS_DB) FLUSHDB; \
	fi

dev-reset:
	@DB_HOST=$(DB_HOST) DB_PORT=$(DB_PORT) DB_USER=$(DB_USER) DB_PASSWORD=$(DB_PASSWORD) DB_NAME=$(DB_NAME) \
	  REDIS_HOST=$(REDIS_HOST) REDIS_PORT=$(REDIS_PORT) REDIS_DB=$(REDIS_DB) REDIS_PASSWORD=$(REDIS_PASSWORD) \
	  ./scripts/dev_reset.sh

dev-stop:
	@API_PORT=$(API_PORT) WS_PORT=$(WS_PORT) LLM_PORT=$(LLM_PORT) PUSH_PORT=$(PUSH_PORT) GATEWAY_PORT=$(GATEWAY_PORT) \
	  ./scripts/dev_stop.sh

dev-up:
	@CONFIG_PATH=$(if $(CONFIG),$(CONFIG),config.yaml) \
	  API_PORT=$(API_PORT) WS_PORT=$(WS_PORT) LLM_PORT=$(LLM_PORT) PUSH_PORT=$(PUSH_PORT) GATEWAY_PORT=$(GATEWAY_PORT) \
	  ./scripts/dev_up.sh

dev-restart: dev-up

# Validate Agent API websocket JSON schema examples
schema-verify:
	@set -e; \
	if [ ! -f "$(SCHEMA_VERIFY_SCRIPT)" ]; then \
		echo "schema verify script not found: $(SCHEMA_VERIFY_SCRIPT)"; \
		exit 1; \
	fi; \
	bash "$(SCHEMA_VERIFY_SCRIPT)"

# Clean build artifacts
clean:
	rm -rf bin/

# Show all supported commands
help:
	@printf '%s\n' \
		'Build:' \
		'  make build                    Build all services (api, ws, llm, push, gateway, migrate, voicebridge)' \
		'  make build-api                Build API service -> bin/api' \
		'  make build-api-web            Sync web dist then build API service' \
		'  make sync-web-dist            Sync built Flutter Web dist into embedded webapp' \
		'  make generate-well-known      Generate well-known files' \
		'  make build-ws                 Build WebSocket service -> bin/ws' \
		'  make build-llm                Build LLM orchestration service -> bin/llm' \
		'  make build-push               Build push notification worker -> bin/push' \
		'  make build-migrate            Build DB migration tool -> bin/migrate' \
		'  make build-voicebridge        Build voicebridge service -> bin/voicebridge' \
		'  make build-gateway            Build LLM billing/relay gateway -> bin/gateway' \
		'' \
		'Run (dev, go run):' \
		'  make run-api                  Run API service (port $(API_PORT))' \
		'  make run-ws                   Run WebSocket service (port $(WS_PORT))' \
		'  make run-llm                  Run LLM service (port $(LLM_PORT))' \
		'  make run-push                 Run push worker (port $(PUSH_PORT))' \
		'  make run-gateway              Run gateway service (port $(GATEWAY_PORT))' \
		'' \
		'Dev lifecycle:' \
		'  make dev-up                   Start all services in background (loads .env, bootstraps admin)' \
		'  make dev-stop                 Stop all dev services' \
		'  make dev-restart              Restart dev services (alias of dev-up)' \
		'  make dev-reset                Reset DB + Redis + restart' \
		'' \
		'Database & cache:' \
		'  make migrate                  Apply initial SQL migration' \
		'  make reset-db                 Drop+recreate schema, re-apply migrations' \
		'  make flush-redis              Flush Redis DB' \
		'' \
		'Dependencies:' \
		'  make tidy                     go mod tidy' \
		'' \
		'Validation:' \
		'  make schema-verify            Validate Agent API websocket JSON schema examples' \
		'' \
		'Clean:' \
		'  make clean                    Remove bin/' \
		'' \
		'Variables:' \
		'  DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME     Default: $(DB_HOST) / $(DB_PORT) / $(DB_USER) / (hidden) / $(DB_NAME)' \
		'  REDIS_HOST/REDIS_PORT/REDIS_DB/REDIS_PASSWORD   Default: $(REDIS_HOST) / $(REDIS_PORT) / $(REDIS_DB) / (empty)' \
		'  CONFIG=<path>                 Config file passed to run-* targets / dev-up (default: config.yaml)'

# Fallback for any target with no matching rule: show the error plus the full command list.
%:
	@echo "make: *** No rule to make target '$@'." >&2
	@echo "" >&2
	@"$(MAKE)" --no-print-directory help
	@exit 2
