package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	pushPath          = "/api/v1/push"
	kindRequestPos    = "REQUEST_POSITION"
	maxErrorBodyBytes = 4096
)

// базовые ошибки гейтвея, чтобы различать сбой сети, 4xx и 5xx
var (
	ErrTransport    = errors.New("push service transport error")
	ErrClientStatus = errors.New("push service 4xx error")
	ErrServerStatus = errors.New("push service 5xx error")
)

// подробная ошибка ответа push-сервиса с кодом статуса и телом
type StatusError struct {
	StatusCode int
	Body       string
	RetryAfter time.Duration
}

func (e *StatusError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("push service returned status %d: %s", e.StatusCode, e.Body)
	}
	return fmt.Sprintf("push service returned status %d", e.StatusCode)
}

func (e *StatusError) Unwrap() error {
	if e.StatusCode >= http.StatusInternalServerError {
		return ErrServerStatus
	}
	return ErrClientStatus
}

// параметры http-клиента push-сервиса
type Config struct {
	BaseURL        string
	Timeout        time.Duration
	TracerProvider trace.TracerProvider
	Propagator     propagation.TextMapPropagator
}

// http-гейтвей для отправки команд в push-service
type Client struct {
	baseURL    string
	httpClient *http.Client
}

type pushRequest struct {
	RequestID   string            `json:"request_id"`
	RecipientID string            `json:"recipient_id"`
	Kind        string            `json:"kind"`
	Data        map[string]string `json:"data,omitempty"`
}

// создаём клиент со своим http.Client и транспортом otelhttp для трейсинга исходящих вызовов
func New(cfg Config) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return nil, errors.New("push gateway base url is required")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = time.Second
	}

	baseTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
	}

	opts := []otelhttp.Option{
		otelhttp.WithMeterProvider(noop.NewMeterProvider()),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + pushPath
		}),
	}
	if cfg.TracerProvider != nil {
		opts = append(opts, otelhttp.WithTracerProvider(cfg.TracerProvider))
	}
	if cfg.Propagator != nil {
		opts = append(opts, otelhttp.WithPropagators(cfg.Propagator))
	}

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: otelhttp.NewTransport(baseTransport, opts...),
		},
	}, nil
}

// отправляем команду REQUEST_POSITION водителю для указанной поездки
func (c *Client) RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error {
	payload := pushRequest{
		RequestID:   uuid.NewString(),
		RecipientID: driverID.String(),
		Kind:        kindRequestPos,
		Data: map[string]string{
			"trip_id": tripID.String(),
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal push request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+pushPath, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build push http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTransport, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusAccepted {
		return nil
	}

	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	statusErr := &StatusError{
		StatusCode: resp.StatusCode,
		Body:       strings.TrimSpace(string(snippet)),
	}
	if ra := strings.TrimSpace(resp.Header.Get("Retry-After")); ra != "" {
		if secs, parseErr := strconv.Atoi(ra); parseErr == nil && secs > 0 {
			statusErr.RetryAfter = time.Duration(secs) * time.Second
		}
	}

	return statusErr
}

// закрываем простаивающие keep-alive соединения при остановке сервиса
func (c *Client) Close() {
	if c == nil || c.httpClient == nil {
		return
	}
	c.httpClient.CloseIdleConnections()
}
