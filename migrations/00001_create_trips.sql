-- +goose Up
CREATE TABLE IF NOT EXISTS trips (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    driver_id UUID NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'completed')),
    price INTEGER NOT NULL CHECK (price >= 0),
    start_lat DOUBLE PRECISION NOT NULL CHECK (start_lat >= -90.0 AND start_lat <= 90.0),
    start_lng DOUBLE PRECISION NOT NULL CHECK (start_lng >= -180.0 AND start_lng <= 180.0),
    end_lat DOUBLE PRECISION NOT NULL CHECK (end_lat >= -90.0 AND end_lat <= 90.0),
    end_lng DOUBLE PRECISION NOT NULL CHECK (end_lng >= -180.0 AND end_lng <= 180.0),
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    last_position_at TIMESTAMPTZ,
    CONSTRAINT check_trip_status_finished CHECK (
        (status = 'active' AND finished_at IS NULL) OR
        (status = 'completed' AND finished_at IS NOT NULL)
    )
);

-- частичный уникальный индекс: у одного водителя не может быть больше одной активной поездки одновременно
CREATE UNIQUE INDEX IF NOT EXISTS idx_trips_driver_active 
    ON trips (driver_id) 
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS trip_status_history (
    id BIGSERIAL PRIMARY KEY,
    trip_id UUID NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    changed_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_trip_status_history_trip_id 
    ON trip_status_history (trip_id);

-- +goose Down
DROP TABLE IF EXISTS trip_status_history;
DROP INDEX IF EXISTS idx_trips_driver_active;
DROP TABLE IF EXISTS trips;