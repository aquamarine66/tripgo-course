# подтягиваем переменные из .env, который создаёт tripgoctl environment start
-include .env
export

MIGRATIONS_DIR := migrations

.PHONY: generate migrate migrate-down migrate-status run build test tidy check-db-url

# генерация типов и роутера из openapi
generate:
	mkdir -p internal/api
	go tool oapi-codegen -config contracts/openapi/oapi-codegen.yaml contracts/openapi/trip-service.openapi.yaml

# накатить все миграции
migrate: check-db-url
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" up

# откатить последнюю миграцию
migrate-down: check-db-url
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" down

# посмотреть статус миграций
migrate-status: check-db-url
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" status

# запустить сервис локально (перед запуском обновляем сгенерированный код)
run: generate
	go run ./cmd/trip-service

# собрать бинарь в bin/ с предварительной кодогенерацией
build: generate
	mkdir -p bin
	go build -o bin/trip-service ./cmd/trip-service

# прогнать тесты с детектором гонок
test: generate
	go test -race ./...

tidy:
	go mod tidy

check-db-url:
	@test -n "$(DATABASE_URL)" || { echo "DATABASE_URL is not set: run 'tripgoctl environment start' or 'cp .env.example .env'"; exit 1; }
