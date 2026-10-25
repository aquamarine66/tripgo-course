# TripGo — trip-service

Сервис управления поездками (Лабораторная работа 1 курса «Разработка микросервисов на Go»).

## Требования
- Go 1.22+
- PostgreSQL 16+
- Goose (для применения миграций схемы)

## Конфигурация
Создайте локальный конфигурационный файл на основе шаблона:
```bash
cp .env.example .env
```

Параметры окружения:
- `HTTP_ADDR`: адрес HTTP-сервера (по умолчанию `:8080`)
- `DATABASE_URL`: строка подключения к PostgreSQL (`postgres://tripgo:tripgo@localhost:5432/tripgo?sslmode=disable`)

## Миграции базы данных
Применение миграций схемы поездок:
```bash
goose -dir migrations/trip postgres "$DATABASE_URL" up
```

Откат последней миграции:
```bash
goose -dir migrations/trip postgres "$DATABASE_URL" down
```

## Сборка и запуск сервиса
Компиляция проекта:
```bash
go build ./...
```

Запуск сервиса:
```bash
go run ./cmd/trip-service
```

## Проверка работоспособности
- **Liveness-проба:**
  ```bash
  curl -i http://localhost:8080/health
  ```
- **Readiness-проба (проверка соединения с PostgreSQL):**
  ```bash
  curl -i http://localhost:8080/ready
  ```
- **Создание поездки:**
  ```bash
  curl -i -X POST http://localhost:8080/api/v1/trips \
    -H "Content-Type: application/json" \
    -d '{
      "user_id": "00000000-0000-0000-0000-000000000001",
      "driver_id": "00000000-0000-0000-0000-000000000002",
      "price": 450,
      "start_point": {"latitude": 59.9343, "longitude": 30.3351},
      "end_point": {"latitude": 59.9400, "longitude": 30.3400}
    }'
  ```