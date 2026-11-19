-- +goose NO TRANSACTION
-- +goose Up
-- частичный индекс под выборку активных поездок без свежих координат
CREATE INDEX CONCURRENTLY IF NOT EXISTS trips_active_last_activity_idx
    ON trips ((COALESCE(last_position_at, started_at)))
    WHERE status = 'active';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS trips_active_last_activity_idx;