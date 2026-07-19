#################################################################################
# BUILD COMMANDS
#################################################################################
.PHONY: build
build: build-cli build-registry # Build all binaries into bin/.

.PHONY: build-cli
build-cli: # Build the evoke CLI.
	go build -o bin/evoke ./cmd/cli

.PHONY: build-registry
build-registry: # Build the registry API server.
	go build -o bin/evoke-registry ./cmd/app

.PHONY: generate
generate: # Regenerate ent code from the schema.
	go generate ./...

#################################################################################
# RUN COMMANDS
#################################################################################
.PHONY: run
run: # Run the registry API against the local docker-compose Postgres. Loads .env if present.
	set -a; if [ -f .env ]; then . ./.env; fi; set +a; \
	AUTH_SECRET_KEY=$${AUTH_SECRET_KEY:-dev-secret-change-me} \
	POSTGRES_HOST=$${POSTGRES_HOST:-localhost} \
	POSTGRES_USER=$${POSTGRES_USER:-evoke} \
	POSTGRES_PASSWORD=$${POSTGRES_PASSWORD:-evoke} \
	POSTGRES_DB=$${POSTGRES_DB:-evoke} \
	POSTGRES_SSLMODE=$${POSTGRES_SSLMODE:-disable} \
	go run ./cmd/app

.PHONY: up
up: # Start the local stack (Postgres + registry) in the background.
	docker compose up --build -d

.PHONY: down
down: # Stop the local stack and remove volumes.
	docker compose down -v

.PHONY: logs
logs: # Tail the local stack logs.
	docker compose logs -f

#################################################################################
# TEST / LINT COMMANDS
#################################################################################
.PHONY: tidy
tidy: # Sync go.mod / go.sum.
	go mod tidy

.PHONY: lint
lint: # Run the linter and vulnerability checker.
	go mod tidy
	golangci-lint run ./...
	govulncheck ./...

.PHONY: test
test: # Run the tests.
	go test -cover ./... -timeout 60s
