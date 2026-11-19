package positions

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

const (
	resultQueued    = "queued"
	resultDuplicate = "duplicate"
	resultDropped   = "dropped"
	resultSuccess   = "success"
	resultError     = "error"
)

// настройки сканера, очереди и пула воркеров
type Config struct {
	CheckInterval      time.Duration
	StaleAfter         time.Duration
	Workers            int
	QueueSize          int
	PushRequestTimeout time.Duration
}

type task struct {
	tripID        uuid.UUID
	driverID      uuid.UUID
	token         uint64
	sourceSpanCtx trace.SpanContext
}

type realTicker struct {
	t *time.Ticker
}

func (r *realTicker) C() <-chan time.Time { return r.t.C }
func (r *realTicker) Stop()               { r.t.Stop() }

// объединяет сканер по тикеру, ограниченную очередь с дедупликацией и фиксированный пул воркеров
type Pool struct {
	cfg       Config
	finder    staleTripFinder
	pusher    pushClient
	metrics   poolMetrics
	tracer    trace.Tracer
	logger    *slog.Logger
	newTicker func(time.Duration) ticker

	queue chan task

	mu        sync.Mutex
	pending   map[uuid.UUID]uint64
	nextToken uint64

	activeWorkers atomic.Int64

	startOnce       sync.Once
	stopScannerOnce sync.Once
	scanCancel      context.CancelFunc
	workerCancel    context.CancelFunc
	scannerWG       sync.WaitGroup
	workersWG       sync.WaitGroup
}

// собираем компонент опроса устаревших поездок
func New(
	cfg Config,
	finder staleTripFinder,
	pusher pushClient,
	metrics poolMetrics,
	tracer trace.Tracer,
	logger *slog.Logger,
) *Pool {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 30 * time.Second
	}
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = 60 * time.Second
	}
	if cfg.Workers < 1 {
		cfg.Workers = 4
	}
	if cfg.QueueSize < 1 {
		cfg.QueueSize = 100
	}
	if cfg.PushRequestTimeout <= 0 {
		cfg.PushRequestTimeout = time.Second
	}
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("positions")
	}

	return &Pool{
		cfg:     cfg,
		finder:  finder,
		pusher:  pusher,
		metrics: metrics,
		tracer:  tracer,
		logger:  logger,
		newTicker: func(d time.Duration) ticker {
			return &realTicker{t: time.NewTicker(d)}
		},
		queue:   make(chan task, cfg.QueueSize),
		pending: make(map[uuid.UUID]uint64, cfg.QueueSize),
	}
}

// запускаем фиксированное число воркеров и одну фоновую горутину сканера
func (p *Pool) Start(ctx context.Context) {
	p.startOnce.Do(func() {
		scanCtx, scanCancel := context.WithCancel(ctx)
		// воркеры живут чуть дольше сканера, чтобы успеть доразбирать уже поставленные задачи
		workerCtx, workerCancel := context.WithCancel(context.WithoutCancel(ctx))

		p.mu.Lock()
		p.scanCancel = scanCancel
		p.workerCancel = workerCancel
		p.mu.Unlock()

		if p.logger != nil {
			p.logger.InfoContext(ctx, "position worker pool started",
				"workers", p.cfg.Workers,
				"queue_size", p.cfg.QueueSize,
			)
		}

		for range p.cfg.Workers {
			p.workersWG.Add(1)
			go p.runWorker(workerCtx)
		}

		p.scannerWG.Add(1)
		go p.runScanner(scanCtx)
	})
}

// убираем поездку из очереди опроса, когда по ней пришла свежая координата или она завершилась
func (p *Pool) Cancel(tripID uuid.UUID) {
	p.mu.Lock()
	delete(p.pending, tripID)
	p.mu.Unlock()
}

// останавливаем сканер, ждём завершения текущего тика и закрываем канал очереди
func (p *Pool) StopScanner() {
	p.stopScannerOnce.Do(func() {
		p.mu.Lock()
		cancel := p.scanCancel
		p.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		p.scannerWG.Wait()
	})
}

// ждём, пока воркеры доработают оставшиеся в очереди задачи в пределах ctx; при таймауте обрываем их
func (p *Pool) WaitWorkers(ctx context.Context) error {
	p.scannerWG.Wait()

	done := make(chan struct{})
	go func() {
		p.workersWG.Wait()
		close(done)
	}()

	var waitErr error
	select {
	case <-done:
	case <-ctx.Done():
		waitErr = ctx.Err()
		p.cancelWorkers()
		<-done
	}

	p.cancelWorkers()

	if p.logger != nil {
		logCtx := context.WithoutCancel(ctx)
		if waitErr != nil {
			p.logger.WarnContext(logCtx, "position worker pool stopped on timeout", "error", waitErr)
		} else {
			p.logger.InfoContext(logCtx, "position worker pool stopped")
		}
	}

	return waitErr
}

func (p *Pool) cancelWorkers() {
	p.mu.Lock()
	cancel := p.workerCancel
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (p *Pool) runScanner(ctx context.Context) {
	tk := p.newTicker(p.cfg.CheckInterval)
	defer func() {
		tk.Stop()
		close(p.queue)
		if p.logger != nil {
			p.logger.InfoContext(context.WithoutCancel(ctx), "position scanner stopped")
		}
		p.scannerWG.Done()
	}()

	if p.logger != nil {
		p.logger.InfoContext(ctx, "position scanner started",
			"check_interval", p.cfg.CheckInterval.String(),
			"stale_after", p.cfg.StaleAfter.String(),
		)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C():
			p.scanOnce(ctx)
		}
	}
}

func (p *Pool) scanOnce(ctx context.Context) {
	scanCtx, span := p.tracer.Start(ctx, "positions.scan",
		trace.WithNewRoot(),
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	trips, err := p.finder.FindStaleActive(scanCtx, p.cfg.StaleAfter)
	if err != nil {
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			return
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if p.logger != nil {
			p.logger.ErrorContext(scanCtx, "failed to find stale active trips", "error", err)
		}
		return
	}

	span.SetAttributes(attribute.Int("tripgo.stale.count", len(trips)))
	sourceSpanCtx := span.SpanContext()

	for _, st := range trips {
		p.enqueue(scanCtx, st, sourceSpanCtx)
	}
}

func (p *Pool) enqueue(ctx context.Context, st model.StaleTrip, sourceSpanCtx trace.SpanContext) {
	p.mu.Lock()
	if _, exists := p.pending[st.TripID]; exists {
		p.mu.Unlock()
		p.incStaleCheck(ctx, resultDuplicate)
		return
	}

	p.nextToken++
	tok := p.nextToken
	t := task{
		tripID:        st.TripID,
		driverID:      st.DriverID,
		token:         tok,
		sourceSpanCtx: sourceSpanCtx,
	}

	select {
	case p.queue <- t:
		p.pending[st.TripID] = tok
		qLen := int64(len(p.queue))
		p.mu.Unlock()

		p.setQueueSize(ctx, qLen)
		p.incStaleCheck(ctx, resultQueued)
	default:
		qLen := int64(len(p.queue))
		p.mu.Unlock()

		p.setQueueSize(ctx, qLen)
		p.incStaleCheck(ctx, resultDropped)
		p.incPositionRequest(ctx, resultDropped)
		if p.logger != nil {
			p.logger.WarnContext(ctx, "position request queue is full, dropping task",
				"trip_id", st.TripID.String(),
				"driver_id", st.DriverID.String(),
				"queue_size", qLen,
			)
		}
	}
}

func (p *Pool) runWorker(ctx context.Context) {
	defer p.workersWG.Done()

	for t := range p.queue {
		if ctx.Err() != nil {
			p.clearPending(t)
			p.setQueueSize(context.WithoutCancel(ctx), int64(len(p.queue)))
			continue
		}
		p.processTask(ctx, t)
	}
}

func (p *Pool) processTask(ctx context.Context, t task) {
	p.mu.Lock()
	tok, exists := p.pending[t.tripID]
	shouldRun := exists && tok == t.token
	qLen := int64(len(p.queue))
	p.mu.Unlock()

	p.setQueueSize(ctx, qLen)
	if !shouldRun {
		return
	}
	defer p.clearPending(t)

	// связываем асинхронную обработку со спаном сканера через span link, а не родителя
	spanOpts := []trace.SpanStartOption{
		trace.WithNewRoot(),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("trip.id", t.tripID.String()),
			attribute.String("driver.id", t.driverID.String()),
		),
	}
	if t.sourceSpanCtx.IsValid() {
		spanOpts = append(spanOpts, trace.WithLinks(trace.Link{SpanContext: t.sourceSpanCtx}))
	}

	taskCtx, span := p.tracer.Start(ctx, "positions.request", spanOpts...)
	defer span.End()

	active := p.activeWorkers.Add(1)
	p.setWorkersActive(taskCtx, active)
	defer func() {
		remaining := p.activeWorkers.Add(-1)
		p.setWorkersActive(context.WithoutCancel(taskCtx), remaining)
	}()

	reqCtx, cancel := context.WithTimeout(taskCtx, p.cfg.PushRequestTimeout)
	defer cancel()

	started := time.Now()
	err := p.pusher.RequestPosition(reqCtx, t.tripID, t.driverID)
	dur := time.Since(started).Seconds()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		p.incPositionRequest(taskCtx, resultError)
		p.observeRequestDuration(taskCtx, dur, resultError)
		if p.logger != nil {
			p.logger.WarnContext(taskCtx, "push service request failed",
				"trip_id", t.tripID.String(),
				"driver_id", t.driverID.String(),
				"error", err,
			)
		}
		return
	}

	p.incPositionRequest(taskCtx, resultSuccess)
	p.observeRequestDuration(taskCtx, dur, resultSuccess)
}

func (p *Pool) clearPending(t task) {
	p.mu.Lock()
	if cur, ok := p.pending[t.tripID]; ok && cur == t.token {
		delete(p.pending, t.tripID)
	}
	p.mu.Unlock()
}

func (p *Pool) incStaleCheck(ctx context.Context, result string) {
	if p.metrics != nil {
		p.metrics.IncStaleCheck(ctx, result)
	}
}

func (p *Pool) setQueueSize(ctx context.Context, size int64) {
	if p.metrics != nil {
		p.metrics.SetPositionQueueSize(ctx, size)
	}
}

func (p *Pool) setWorkersActive(ctx context.Context, active int64) {
	if p.metrics != nil {
		p.metrics.SetPositionWorkersActive(ctx, active)
	}
}

func (p *Pool) incPositionRequest(ctx context.Context, result string) {
	if p.metrics != nil {
		p.metrics.IncPositionRequest(ctx, result)
	}
}

func (p *Pool) observeRequestDuration(ctx context.Context, seconds float64, result string) {
	if p.metrics != nil {
		p.metrics.ObservePositionRequestDuration(ctx, seconds, result)
	}
}
