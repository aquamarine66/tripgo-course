# TripGo — trip-service

Сервис управления поездками (лабораторная работа 1 курса «Разработка микросервисов на Go»):
HTTP API по контракту OpenAPI, хранение в PostgreSQL, собственный менеджер транзакций.

## Требования

- Go 1.26+ (версия из `go.mod`; `oapi-codegen` и `goose` подключены как tools модуля
  и запускаются через `go tool`, ставить их глобально не нужно)
- [`tripgoctl`](https://github.com/course-go-autumn-2026) — поднимает PostgreSQL
- `make`, `curl` (и по желанию `jq`) для проверки

## Запуск из чистого клона

```bash
git clone git@github.com:aquamarine66/tripgo-course.git
cd tripgo-course
git checkout homework/01

# 1. окружение: поднимает PostgreSQL и создаёт .env с его адресом
tripgoctl cluster start          # один раз на машине
tripgoctl environment start
tripgoctl connect                # адреса компонентов

# 2. миграции и запуск
make migrate
make run
```

`Makefile` подхватывает переменные из `.env`, который создаёт
`tripgoctl environment start`. Если запускаете без `tripgoctl` — скопируйте
шаблон и поправьте `DATABASE_URL` под свою базу:

```bash
cp .env.example .env
```

## Цели Makefile

| Цель | Что делает |
|---|---|
| `make generate` | генерирует `internal/api/api.gen.go` из `contracts/openapi/trip-service.openapi.yaml` |
| `make migrate` | накатывает все миграции (`goose up`) |
| `make migrate-down` | откатывает последнюю миграцию (`goose down`) |
| `make migrate-status` | статус миграций |
| `make run` | запускает сервис |
| `make build` | собирает бинарь в `bin/trip-service` |
| `make test` | все тесты с `-race` |
| `make lint` | `golangci-lint` (нужен установленный бинарь) |

## Переменные окружения

Все параметры читаются из окружения. Обязательная переменная — `DATABASE_URL`.
Если переменная задана с некорректным значением (например, `DATABASE_MAX_CONNS=abc`
или `SHUTDOWN_TIMEOUT=0`), сервис не стартует и пишет, что именно не так.

| Переменная | По умолчанию | Описание |
|---|---|---|
| `DATABASE_URL` | — (обязательна) | строка подключения к PostgreSQL |
| `HTTP_ADDR` | `:8080` | адрес HTTP-сервера |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `SHUTDOWN_TIMEOUT` | `10s` | общий бюджет graceful shutdown |
| `HTTP_READ_TIMEOUT` | `5s` | `http.Server.ReadTimeout` |
| `HTTP_READ_HEADER_TIMEOUT` | `2s` | `http.Server.ReadHeaderTimeout` |
| `HTTP_WRITE_TIMEOUT` | `10s` | `http.Server.WriteTimeout` |
| `HTTP_IDLE_TIMEOUT` | `120s` | `http.Server.IdleTimeout` |
| `DATABASE_MAX_CONNS` | `10` | максимум соединений в пуле (≥ 1) |
| `DATABASE_MIN_CONNS` | `2` | минимум соединений в пуле (≤ `DATABASE_MAX_CONNS`) |
| `DATABASE_MAX_CONN_LIFETIME` | `30m` | время жизни соединения |
| `DATABASE_CONNECT_TIMEOUT` | `5s` | таймаут установки соединения и стартового `Ping` |
| `DATABASE_QUERY_TIMEOUT` | `3s` | таймаут каждого запроса к бд и проверки в `/ready` |

Полный список переменных курса (для следующих работ) — в `.env.example`.

## Тесты

```bash
make test
```

Интеграционные тесты менеджера транзакций (откат при ошибке и панике, вложенный
`Do`, атомарность создания поездки) ходят в настоящую бд и без неё пропускаются.
Чтобы их запустить, нужна бд с накатанными миграциями:

```bash
make migrate
TEST_DATABASE_URL="$(grep ^DATABASE_URL= .env | cut -d= -f2-)" go test -race -count=1 ./internal/repository/
```

## API

Контракт — `contracts/openapi/trip-service.openapi.yaml`. Реализовано в этой работе:

| Метод | Путь | Ответы |
|---|---|---|
| `POST` | `/api/v1/trips` | `201` + `Location`, `400`, `409`, `500` |
| `GET` | `/api/v1/trips/{tripId}` | `200`, `400`, `404`, `500` |
| `POST` | `/api/v1/trips/{tripId}/finish` | `200`, `400`, `404`, `409`, `500` |
| `GET` | `/health` | `200` |
| `GET` | `/ready` | `200`, `503` |

Ручки `/positions` относятся к работе 3 и пока отвечают `501` в формате `problem+json`.

### Проверка

```bash
curl -i localhost:8080/health
curl -i localhost:8080/ready

# создание — 201, заголовок Location
curl -i -X POST localhost:8080/api/v1/trips \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c",
       "driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5",
       "start_point":{"latitude":59.9398,"longitude":30.3146},
       "end_point":{"latitude":59.9290,"longitude":30.3626},
       "price":1450}'

# повтор на том же водителе — 409 driver_busy
# получение
curl -i localhost:8080/api/v1/trips/$TRIP_ID
# завершение — 200, повторное — 409 trip_completed
curl -i -X POST localhost:8080/api/v1/trips/$TRIP_ID/finish
# tripId не UUID — 400 invalid_request
curl -i localhost:8080/api/v1/trips/not-a-uuid
```

Гонка на создание — ровно один `201`, остальные `409`:

```bash
seq 20 | xargs -P20 -I{} curl -s -o /dev/null -w '%{http_code}\n' \
  -X POST localhost:8080/api/v1/trips -H 'content-type: application/json' \
  -d @trip.json | sort | uniq -c
```

### Ошибки

Все ошибки отдаются по RFC 9457 в `application/problem+json` со схемой `Problem`
из контракта. Для каждого `code` фиксированы `type`, `title` и HTTP-статус:

| HTTP | `code` | Когда |
|---|---|---|
| `400` | `invalid_request` | невалидный JSON, нарушение схемы `TripData`, пустой/нулевой UUID, `tripId` не UUID |
| `404` | `trip_not_found` | поездки с таким `id` нет |
| `409` | `trip_completed` | повторное завершение поездки |
| `409` | `driver_busy` | у водителя уже есть активная поездка |
| `500` | `internal_error` | всё остальное |

## Решения

### Структура

```
cmd/trip-service/       main: конфиг, пул, сборка зависимостей, HTTP-сервер, graceful shutdown
internal/config/        чтение и проверка переменных окружения
internal/api/           сгенерированный код (api.gen.go) + хендлеры и problem+json
internal/service/       бизнес-логика; объявляет нужные ей интерфейсы TxManager и TripRepository
internal/repository/    pgx + squirrel, менеджер транзакций
internal/model/         доменные сущности и ошибки
migrations/             миграции goose
contracts/              контракты курса (не редактируются)
```

### Менеджер транзакций

`repository.TxManager.Do(ctx, fn)`:

- открывает транзакцию (`BeginTx` с явным уровнем изоляции), кладёт `pgx.Tx`
  в `context` и вызывает `fn` с этим контекстом;
- `fn` вернула `nil` → `COMMIT`; вернула ошибку → `ROLLBACK` и ошибка
  пробрасывается наружу (если упал и сам `ROLLBACK`, обе ошибки объединяются
  через `errors.Join`);
- `fn` запаниковала → `ROLLBACK` в `defer`/`recover`, после чего паника
  пробрасывается дальше (её ловит `middleware.Recoverer` и отдаёт `500`).
  Соединение не остаётся висеть с открытой транзакцией;
- `ROLLBACK` выполняется с `context.WithoutCancel`, чтобы откат дошёл до бд,
  даже если контекст запроса уже отменён;
- **вложенный `Do`**: если в контексте уже есть транзакция, новая не
  открывается — `fn` выполняется в той же транзакции, а `COMMIT`/`ROLLBACK`
  делает только внешний `Do`. Ошибка внутреннего вызова откатывает всю
  внешнюю транзакцию;
- репозиторий достаёт исполнителя через `getQuerier(ctx)`: есть транзакция
  в контексте — работает через неё, нет — через пул. Транзакция не передаётся
  аргументом методов репозитория. Вызов репозитория вне `Do` просто выполняет
  запрос на пуле в режиме autocommit.

Сервис зависит только от интерфейса `Do(ctx, fn) error`, объявленного в пакете
`service`: про `pgx`, пулы и транзакции бизнес-код не знает.

Создание поездки — один `Do`: `INSERT` в `trips` и `INSERT` в
`trip_status_history`. Завершение — тоже один `Do`: `UPDATE trips` и запись
в журнал. Поведение (откат при ошибке, при панике, вложенный вызов, атомарность
создания при падении второй вставки) покрыто тестами в
`internal/repository/tx_manager_test.go`.

### Уровень изоляции: READ COMMITTED

Выбран `READ COMMITTED` (он же значение по умолчанию в PostgreSQL), но задаётся
явно в `Do`, а не полагается на настройки сервера.

Почему его достаточно:

- инварианты держит сама бд, а не чтение-проверка-запись в коде: запрет двух
  активных поездок — частичный уникальный индекс, завершение — условный `UPDATE`.
  Обе гарантии работают на любом уровне изоляции;
- транзакции короткие: две вставки или `UPDATE` + вставка, без чтений, по
  результатам которых принимается решение. Аномалии, от которых защищают
  `REPEATABLE READ`/`SERIALIZABLE` (неповторяемое чтение, write skew), здесь
  не возникают;
- на более строгих уровнях конкурентные транзакции получали бы ошибки
  сериализации (`40001`), и пришлось бы писать повторы — лишняя сложность без
  выигрыша для этих сценариев.

### Запрет двух активных поездок у водителя

Частичный уникальный индекс в миграции:

```sql
CREATE UNIQUE INDEX trips_driver_active_uniq ON trips (driver_id) WHERE status = 'active';
```

Проверка `SELECT` перед `INSERT` не спасает от гонки: оба запроса пройдут её
раньше, чем любой вставит строку. Индекс же проверяется самой бд при вставке:
из двух параллельных `INSERT` второй ждёт первый и получает `23505
unique_violation`. Репозиторий распознаёт код `23505` именно на этом индексе
(`ConstraintName`) и возвращает доменную ошибку `ErrDriverAlreadyHasActiveTrip`
→ `409 driver_busy`. Завершённые поездки в индекс не попадают, поэтому после
завершения водитель может взять новую.

### Конкурентное завершение поездки

Статус проверяется условием прямо в `UPDATE`:

```sql
UPDATE trips SET status = 'completed', finished_at = $1, updated_at = $1
WHERE id = $2 AND status = 'active'
RETURNING ...
```

Второй параллельный `UPDATE` ждёт блокировку строки, после коммита первого
перечитывает её, условие `status = 'active'` уже не выполняется — обновлено
0 строк, `finished_at` не перезаписывается. Ноль строк означает «поездки нет»
или «уже завершена»; это различается дополнительным `SELECT status` →
`404 trip_not_found` или `409 trip_completed`. Если убрать условие по статусу,
оба запроса вернут `200`, и второй перезапишет `finished_at` и добавит лишнюю
запись в журнал.

### Валидация запроса

Тело `POST /api/v1/trips` проверяется до бизнес-логики, по схеме `TripData`
из спецификации, встроенной в сгенерированный код (`GetSwagger()`, проверка
через `kin-openapi`). Поэтому валидация всегда совпадает с контрактом:

- отсутствующие обязательные поля (`user_id`, `driver_id`, `start_point`,
  `end_point`, `price`, `latitude`, `longitude`) → `400`. Без этого отсутствие
  поля превращалось бы в нулевое значение (`price = 0`, координаты `0,0`);
- неизвестные поля, в том числе вложенные (`additionalProperties: false`) → `400`;
- диапазоны координат и `price >= 0` → `400`;
- после схемы тело разбирается в сгенерированный `TripData` — здесь проверяется
  формат UUID; пустая строка и нулевой UUID `00000000-...` также отклоняются.

`tripId` в пути разбирает сгенерированный роутер; невалидный UUID уходит в
`api.ParamErrorHandler` и превращается в `400 invalid_request` в формате
`problem+json` (по умолчанию сгенерированный код отдаёт `text/plain`).

### Ошибки и внутренние детали

Перевод доменных ошибок в HTTP собран в одном месте — `writeError` в
`internal/api/problem.go`. Неизвестная ошибка логируется целиком (`slog`),
а клиенту уходит только `500 internal_error` с текстом «Internal server error»:
тексты ошибок бд и SQL наружу не попадают.

### Работа с бд

- `pgxpool`, параметры пула и таймауты — из env; стартовый `Ping` с
  `DATABASE_CONNECT_TIMEOUT`, при недоступности бд сервис не стартует;
- SQL строится `squirrel` с плейсхолдерами `$N`, все значения параметризованы;
- каждый запрос выполняется с таймаутом `DATABASE_QUERY_TIMEOUT`, `/ready`
  пингует бд с тем же таймаутом;
- схема — строго по `contracts/schema.md` (имена колонок, `BIGINT` для цены,
  `created_at`/`updated_at`, `from_status`/`to_status`/`reason` в журнале,
  индексы). `last_position_at` появится отдельной миграцией в работе 3.

### `/health` и `/ready`

`/health` — liveness: отвечает `200`, пока жив процесс, и в бд не ходит —
иначе при недоступной бд оркестратор начал бы перезапускать здоровые поды,
что не помогает. `/ready` — readiness: пингует бд и при её недоступности
отвечает `503`, чтобы на под временно не слали трафик. Обе ручки отвечают
`application/json` со схемой `HealthResponse`.

### Graceful shutdown

По `SIGINT`/`SIGTERM` сервер перестаёт принимать новые соединения и дожидается
текущих запросов (`http.Server.Shutdown`) в пределах `SHUTDOWN_TIMEOUT`; по
истечении бюджета соединения закрываются принудительно с записью в лог. После
этого закрывается пул бд.
