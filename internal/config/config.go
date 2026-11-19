package config

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// конфиг сервиса, всё читаем только из переменных окружения
type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	LogLevel        string
	ShutdownTimeout time.Duration

	// таймауты http-сервера
	HTTPReadTimeout       time.Duration
	HTTPReadHeaderTimeout time.Duration
	HTTPWriteTimeout      time.Duration
	HTTPIdleTimeout       time.Duration

	// настройки пула подключений к postgres
	DBMaxConns        int32
	DBMinConns        int32
	DBConnectTimeout  time.Duration
	DBQueryTimeout    time.Duration
	DBMaxConnLifetime time.Duration

	// настройки opentelemetry
	OTelServiceName      string
	OTelServiceVersion   string
	OTelServiceNamespace string
	OTelEnvironment      string
	OTelExporterEndpoint string
	OTelExporterProtocol string
	OTelTracesSampler    string
	OTelTracesSamplerArg float64

	// настройки опроса устаревших поездок и гейтвея пушей
	PositionCheckInterval time.Duration
	PositionStaleAfter    time.Duration
	PositionWorkers       int
	PositionQueueSize     int
	PushHTTPURL           string
	PushRequestTimeout    time.Duration
}

// читаем переменные окружения и валидируем значения до старта сервиса
func Load() (*Config, error) {
	p := &parser{}

	ns, env := parseResourceAttributes(os.Getenv("OTEL_RESOURCE_ATTRIBUTES"))

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

		OTelServiceName:      p.str("OTEL_SERVICE_NAME", "trip-service"),
		OTelServiceVersion:   p.str("OTEL_SERVICE_VERSION", "0.3.0"),
		OTelServiceNamespace: ns,
		OTelEnvironment:      env,
		OTelExporterEndpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		OTelExporterProtocol: p.oneOf("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf", "http/protobuf", "grpc"),
		OTelTracesSampler: p.oneOf(
			"OTEL_TRACES_SAMPLER",
			"parentbased_always_on",
			"always_on",
			"always_off",
			"traceidratio",
			"parentbased_always_on",
			"parentbased_always_off",
			"parentbased_traceidratio",
		),
		OTelTracesSamplerArg: p.float64Range("OTEL_TRACES_SAMPLER_ARG", 1.0, 0.0, 1.0),

		PositionCheckInterval: p.duration("POSITION_CHECK_INTERVAL", 30*time.Second),
		PositionStaleAfter:    p.duration("POSITION_STALE_AFTER", 60*time.Second),
		PositionWorkers:       p.positiveInt("POSITION_WORKERS", 4),
		PositionQueueSize:     p.positiveInt("POSITION_QUEUE_SIZE", 100),
		PushHTTPURL:           p.httpURL("PUSH_HTTP_URL"),
		PushRequestTimeout:    p.duration("PUSH_REQUEST_TIMEOUT", time.Second),
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

func parseResourceAttributes(raw string) (namespace, environment string) {
	namespace = "tripgo"
	environment = "local"
	for _, pair := range strings.Split(raw, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "service.namespace":
			if v != "" {
				namespace = strings.TrimSpace(v)
			}
		case "deployment.environment":
			if v != "" {
				environment = strings.TrimSpace(v)
			}
		}
	}
	return namespace, environment
}

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

func (p *parser) httpURL(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		p.errs = append(p.errs, fmt.Errorf("%s is required", key))
		return ""
	}
	u, err := url.ParseRequestURI(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		p.errs = append(p.errs, fmt.Errorf("%s must be a valid http(s) URL, got %q", key, v))
		return ""
	}
	return strings.TrimRight(v, "/")
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

func (p *parser) positiveInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		p.errs = append(p.errs, fmt.Errorf("%s must be an integer >= 1, got %q", key, v))
		return def
	}
	return n
}

func (p *parser) float64Range(key string, def, minVal, maxVal float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < minVal || f > maxVal {
		p.errs = append(p.errs, fmt.Errorf("%s must be a number in [%g, %g], got %q", key, minVal, maxVal, v))
		return def
	}
	return f
}

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
