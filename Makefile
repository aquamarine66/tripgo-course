# Переменные окружения берём из .env, который создаёт `tripgoctl environment start`.
# Адреса и порты здесь не хардкодим: у каждого своё окружение.
-include .env
export

MIGRATIONS_DIR := migrations

.PHONY: generate migrate migrate-down migrate-status run build test test-integration lint tidy check-db-url

## generate: кодогенерация по OpenAPI (internal/generated/api.gen.go) и моки gomock
generate:
	go tool oapi-codegen -config contracts/openapi/oapi-codegen.yaml contracts/openapi/trip-service.openapi.yaml
	go generate ./internal/...

## migrate: накатить все миграции
migrate: check-db-url
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" up

## migrate-down: откатить последнюю миграцию
migrate-down: check-db-url
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" down

## migrate-status: статус миграций
migrate-status: check-db-url
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" status

## run: запустить сервис локально
run:
	go run ./cmd/trip-service

## build: собрать бинарь в bin/
build:
	go build -o bin/trip-service ./cmd/trip-service

## test: все unit-тесты с -race и выводом итогового покрытия domain и usecase
test:
	go test -race -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | grep -E '(internal/domain|internal/usecase|total:)'
	@rm -f coverage.out

## test-integration: интеграционные тесты репозитория на реальном PostgreSQL
test-integration: check-db-url
	go test -race -tags=integration -v ./internal/repository/...

## lint: golangci-lint
lint:
	golangci-lint run ./...

tidy:
	go mod tidy

check-db-url:
	@test -n "$(DATABASE_URL)" || { echo "DATABASE_URL is not set: run 'tripgoctl environment start' or 'cp .env.example .env'"; exit 1; }
