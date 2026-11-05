package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	LogLevel        string
	ShutdownTimeout time.Duration

	// таймауты HTTP-сервера
	HTTPReadTimeout       time.Duration
	HTTPReadHeaderTimeout time.Duration
	HTTPWriteTimeout      time.Duration
	HTTPIdleTimeout       time.Duration

	// настройки пула соединений БД
	DBMaxConns        int32
	DBMinConns        int32
	DBConnectTimeout  time.Duration
	DBQueryTimeout    time.Duration
	DBMaxConnLifetime time.Duration
}

// Load читает конфигурацию из окружения. Обязательные переменные должны быть
// заданы, а заданные значения — корректны: при опечатке в значении сервис
// не стартует молча со значением по умолчанию, а падает с понятной ошибкой.
func Load() (*Config, error) {
	p := &parser{}

	cfg := &Config{
		HTTPAddr:        p.str("HTTP_ADDR", ":8080"),
		DatabaseURL:     p.required("DATABASE_URL"),
		LogLevel:        p.oneOf("LOG_LEVEL", "info", "debug", "info", "warn", "error"),
		ShutdownTimeout: p.duration("SHUTDOWN_TIMEOUT", 10*time.Second),

		HTTPReadTimeout:       p.duration("HTTP_READ_TIMEOUT", 5*time.Second),
		HTTPReadHeaderTimeout: p.duration("HTTP_READ_HEADER_TIMEOUT", 2*time.Second),
		HTTPWriteTimeout:      p.duration("HTTP_WRITE_TIMEOUT", 10*time.Second),
		HTTPIdleTimeout:       p.duration("HTTP_IDLE_TIMEOUT", 120*time.Second),

		DBMaxConns:        p.int32("DATABASE_MAX_CONNS", 10),
		DBMinConns:        p.int32("DATABASE_MIN_CONNS", 2),
		DBConnectTimeout:  p.duration("DATABASE_CONNECT_TIMEOUT", 5*time.Second),
		DBQueryTimeout:    p.duration("DATABASE_QUERY_TIMEOUT", 3*time.Second),
		DBMaxConnLifetime: p.duration("DATABASE_MAX_CONN_LIFETIME", 30*time.Minute),
	}

	if cfg.DBMaxConns < 1 {
		p.errs = append(p.errs, fmt.Errorf("DATABASE_MAX_CONNS must be >= 1, got %d", cfg.DBMaxConns))
	}
	if cfg.DBMinConns < 0 {
		p.errs = append(p.errs, fmt.Errorf("DATABASE_MIN_CONNS must be >= 0, got %d", cfg.DBMinConns))
	}
	if cfg.DBMinConns > cfg.DBMaxConns {
		p.errs = append(p.errs, fmt.Errorf("DATABASE_MIN_CONNS (%d) must not exceed DATABASE_MAX_CONNS (%d)",
			cfg.DBMinConns, cfg.DBMaxConns))
	}

	if err := errors.Join(p.errs...); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// parser копит ошибки, чтобы сообщить обо всех неверных переменных сразу.
type parser struct {
	errs []error
}

func (p *parser) str(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (p *parser) required(key string) string {
	v := os.Getenv(key)
	if v == "" {
		p.errs = append(p.errs, fmt.Errorf("%s is required", key))
	}
	return v
}

func (p *parser) oneOf(key, def string, allowed ...string) string {
	v := p.str(key, def)
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	p.errs = append(p.errs, fmt.Errorf("%s must be one of %v, got %q", key, allowed, v))
	return def
}

func (p *parser) int32(key string, def int32) int32 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil || n < math.MinInt32 || n > math.MaxInt32 {
		p.errs = append(p.errs, fmt.Errorf("%s must be an integer, got %q", key, v))
		return def
	}
	return int32(n)
}

// duration разбирает положительную длительность: нулевой или отрицательный
// таймаут означал бы «без таймаута» либо мгновенную ошибку.
func (p *parser) duration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s must be a duration like 5s or 1m, got %q", key, v))
		return def
	}
	if d <= 0 {
		p.errs = append(p.errs, fmt.Errorf("%s must be positive, got %q", key, v))
		return def
	}
	return d
}
