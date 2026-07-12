# SignFlow developer tasks.
# On Windows, run these under Git Bash, or run the underlying commands directly.

DATABASE_URL ?= postgres://postgres@localhost:5432/signflow?sslmode=disable

.PHONY: generate
generate: ## Regenerate templ templates and sqlc code
	templ generate
	sqlc generate

.PHONY: build
build: generate ## Build the server binary
	go build -o bin/signflow ./cmd/signflow

.PHONY: run
run: generate ## Run the server (loads .env)
	go run ./cmd/signflow

.PHONY: tidy
tidy: ## Tidy modules
	go mod tidy

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: test
test: ## Run tests
	go test ./...

# --- Migrations (goose CLI; the server also auto-migrates on startup) ---
MIGRATIONS_DIR = db/migrations

.PHONY: migrate-up
migrate-up: ## Apply all migrations
	goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" up

.PHONY: migrate-down
migrate-down: ## Roll back the last migration
	goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" down

.PHONY: migrate-status
migrate-status: ## Show migration status
	goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" status

.PHONY: migrate-new
migrate-new: ## Create a new migration: make migrate-new name=add_widgets
	goose -dir $(MIGRATIONS_DIR) create $(name) sql

.PHONY: help
help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'
