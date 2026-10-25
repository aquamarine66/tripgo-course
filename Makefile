DATABASE_URL ?= postgres://tripgo:tripgo@localhost:21032/tripgo?sslmode=disable
MIGRATIONS_DIR = migrations

.PHONY: generate migrate-up migrate-down run test

generate:
	oapi-codegen -config contracts/openapi/oapi-codegen.yaml contracts/openapi/trip-service.openapi.yaml

migrate-up:
	goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" down

run:
	go run cmd/trip-service/main.go

test:
	go test -v -race ./...