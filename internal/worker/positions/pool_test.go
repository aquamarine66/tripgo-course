package positions

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

type fakeTicker struct {
	ch chan time.Time
}

func newFakeTicker() *fakeTicker {
	return &fakeTicker{ch: make(chan time.Time)}
}

func (f *fakeTicker) C() <-chan time.Time { return f.ch }
func (f *fakeTicker) Stop()               {}
func (f *fakeTicker) tick(t *testing.T) {
	t.Helper()
	select {
	case f.ch <- time.Now():
	case <-time.After(2 * time.Second):
		t.Fatal("scanner did not accept tick in time")
	}
}

type syncMetrics struct {
	mu          sync.Mutex
	staleChecks map[string]int
	posRequests map[string]int
	checkNotify chan string
	reqNotify   chan string
}

func newSyncMetrics() *syncMetrics {
	return &syncMetrics{
		staleChecks: make(map[string]int),
		posRequests: make(map[string]int),
		checkNotify: make(chan string, 128),
		reqNotify:   make(chan string, 128),
	}
}

func (m *syncMetrics) IncStaleCheck(_ context.Context, result string) {
	m.mu.Lock()
	m.staleChecks[result]++
	m.mu.Unlock()
	m.checkNotify <- result
}

func (m *syncMetrics) SetPositionQueueSize(_ context.Context, _ int64)     {}
func (m *syncMetrics) SetPositionWorkersActive(_ context.Context, _ int64) {}

func (m *syncMetrics) IncPositionRequest(_ context.Context, result string) {
	m.mu.Lock()
	m.posRequests[result]++
	m.mu.Unlock()
	m.reqNotify <- result
}

func (m *syncMetrics) ObservePositionRequestDuration(_ context.Context, _ float64, _ string) {}

func (m *syncMetrics) staleCount(result string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.staleChecks[result]
}

func (m *syncMetrics) requestCount(result string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.posRequests[result]
}

func waitEvent(t *testing.T, ch <-chan string, label string) string {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
		return ""
	}
}

func waitSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

// сценарий 2: поездка, уже стоящая в очереди или обработке, повторным тиком не дублируется
func TestPool_DuplicateTripNotEnqueuedTwice(t *testing.T) {
	ctrl := gomock.NewController(t)
	finder := NewMockstaleTripFinder(ctrl)
	pusher := NewMockpushClient(ctrl)
	metrics := newSyncMetrics()
	tk := newFakeTicker()

	trip := model.StaleTrip{TripID: uuid.New(), DriverID: uuid.New()}
	finder.EXPECT().FindStaleActive(gomock.Any(), gomock.Any()).Return([]model.StaleTrip{trip}, nil).Times(2)

	pushStarted := make(chan struct{})
	releasePush := make(chan struct{})
	pusher.EXPECT().RequestPosition(gomock.Any(), trip.TripID, trip.DriverID).
		DoAndReturn(func(_ context.Context, _, _ uuid.UUID) error {
			close(pushStarted)
			<-releasePush
			return nil
		}).Times(1)

	p := New(Config{
		CheckInterval:      time.Second,
		StaleAfter:         time.Minute,
		Workers:            1,
		QueueSize:          4,
		PushRequestTimeout: time.Second,
	}, finder, pusher, metrics, nil, nil)
	p.newTicker = func(time.Duration) ticker { return tk }

	p.Start(t.Context())

	tk.tick(t)
	if got := waitEvent(t, metrics.checkNotify, "queued"); got != resultQueued {
		t.Fatalf("first tick check=%q, want queued", got)
	}
	waitSignal(t, pushStarted, "push started")

	// второй тик приходит, пока первая задача ещё не закончена
	tk.tick(t)
	if got := waitEvent(t, metrics.checkNotify, "duplicate"); got != resultDuplicate {
		t.Fatalf("second tick check=%q, want duplicate", got)
	}

	close(releasePush)
	if got := waitEvent(t, metrics.reqNotify, "success"); got != resultSuccess {
		t.Fatalf("request result=%q, want success", got)
	}

	p.StopScanner()
	if err := p.WaitWorkers(t.Context()); err != nil {
		t.Fatalf("WaitWorkers() error: %v", err)
	}
}

// сценарий 3: приход координаты между постановкой и обработкой отменяет отправку пуша
func TestPool_CancelBetweenEnqueueAndProcessSkipsPush(t *testing.T) {
	ctrl := gomock.NewController(t)
	finder := NewMockstaleTripFinder(ctrl)
	pusher := NewMockpushClient(ctrl)
	metrics := newSyncMetrics()
	tk := newFakeTicker()

	blocker := model.StaleTrip{TripID: uuid.New(), DriverID: uuid.New()}
	canceled := model.StaleTrip{TripID: uuid.New(), DriverID: uuid.New()}

	finder.EXPECT().FindStaleActive(gomock.Any(), gomock.Any()).
		Return([]model.StaleTrip{blocker, canceled}, nil).Times(1)

	blockerStarted := make(chan struct{})
	releaseBlocker := make(chan struct{})
	pusher.EXPECT().RequestPosition(gomock.Any(), blocker.TripID, blocker.DriverID).
		DoAndReturn(func(_ context.Context, _, _ uuid.UUID) error {
			close(blockerStarted)
			<-releaseBlocker
			return nil
		}).Times(1)
	// для canceled вызов RequestPosition не ожидается вообще

	p := New(Config{
		CheckInterval:      time.Second,
		StaleAfter:         time.Minute,
		Workers:            1,
		QueueSize:          4,
		PushRequestTimeout: time.Second,
	}, finder, pusher, metrics, nil, nil)
	p.newTicker = func(time.Duration) ticker { return tk }

	p.Start(t.Context())

	tk.tick(t)
	waitEvent(t, metrics.checkNotify, "first queued")
	waitEvent(t, metrics.checkNotify, "second queued")
	waitSignal(t, blockerStarted, "blocker started")

	// вторая поездка лежит в канале и отменяется до того, как единственный воркер её возьмёт
	p.Cancel(canceled.TripID)
	close(releaseBlocker)
	waitEvent(t, metrics.reqNotify, "blocker finished")

	p.StopScanner()
	if err := p.WaitWorkers(t.Context()); err != nil {
		t.Fatalf("WaitWorkers() error: %v", err)
	}

	if got := metrics.requestCount(resultSuccess); got != 1 {
		t.Fatalf("success requests=%d, want 1", got)
	}
}

// сценарий 4: при переполнении очереди лишние задачи дропаются, сканер не блокируется
func TestPool_QueueOverflowDropsWithoutBlockingScanner(t *testing.T) {
	ctrl := gomock.NewController(t)
	finder := NewMockstaleTripFinder(ctrl)
	pusher := NewMockpushClient(ctrl)
	metrics := newSyncMetrics()
	tk := newFakeTicker()

	trips := make([]model.StaleTrip, 6)
	for i := range trips {
		trips[i] = model.StaleTrip{TripID: uuid.New(), DriverID: uuid.New()}
	}

	scanDone := make(chan struct{})
	finder.EXPECT().FindStaleActive(gomock.Any(), gomock.Any()).Return(trips, nil).Times(1)

	releaseWorkers := make(chan struct{})
	var firstStarted sync.Once
	workerBusy := make(chan struct{})

	pusher.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ uuid.UUID) error {
			firstStarted.Do(func() { close(workerBusy) })
			<-releaseWorkers
			return nil
		}).AnyTimes()

	p := New(Config{
		CheckInterval:      time.Second,
		StaleAfter:         time.Minute,
		Workers:            1,
		QueueSize:          2,
		PushRequestTimeout: time.Second,
	}, finder, pusher, metrics, nil, nil)
	p.newTicker = func(time.Duration) ticker { return tk }

	p.Start(t.Context())

	// сканер не должен заблокироваться, даже когда один воркер держит задачу и канал ёмкостью 2 полон
	go func() {
		tk.tick(t)
		for range len(trips) {
			waitEvent(t, metrics.checkNotify, "stale check result")
		}
		close(scanDone)
	}()

	waitSignal(t, scanDone, "scanner completed without blocking")
	waitSignal(t, workerBusy, "worker busy")

	if dropped := metrics.staleCount(resultDropped); dropped == 0 {
		t.Fatalf("expected dropped stale checks > 0, got 0")
	}
	if droppedReq := metrics.requestCount(resultDropped); droppedReq == 0 {
		t.Fatalf("expected dropped position requests > 0, got 0")
	}

	close(releaseWorkers)
	p.StopScanner()
	if err := p.WaitWorkers(t.Context()); err != nil {
		t.Fatalf("WaitWorkers() error: %v", err)
	}
}

// сценарий 5: пул обрабатывает n задач ровно POSITION_WORKERS воркерами и ставит span link
func TestPool_ProcessesTasksWithFixedWorkersAndSpanLink(t *testing.T) {
	ctrl := gomock.NewController(t)
	finder := NewMockstaleTripFinder(ctrl)
	pusher := NewMockpushClient(ctrl)
	metrics := newSyncMetrics()
	tk := newFakeTicker()

	const (
		numWorkers = 3
		numTasks   = 9
	)

	trips := make([]model.StaleTrip, numTasks)
	for i := range trips {
		trips[i] = model.StaleTrip{TripID: uuid.New(), DriverID: uuid.New()}
	}
	finder.EXPECT().FindStaleActive(gomock.Any(), gomock.Any()).Return(trips, nil).Times(1)

	var (
		inFlight    atomic.Int32
		maxInFlight atomic.Int32
		processed   atomic.Int32
	)
	allBusy := make(chan struct{})
	var allBusyOnce sync.Once
	releaseBatch := make(chan struct{})

	pusher.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ uuid.UUID) error {
			cur := inFlight.Add(1)
			for {
				prev := maxInFlight.Load()
				if cur <= prev || maxInFlight.CompareAndSwap(prev, cur) {
					break
				}
			}
			if cur == numWorkers {
				allBusyOnce.Do(func() { close(allBusy) })
			}
			<-releaseBatch
			inFlight.Add(-1)
			processed.Add(1)
			return nil
		}).Times(numTasks)

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	p := New(Config{
		CheckInterval:      time.Second,
		StaleAfter:         time.Minute,
		Workers:            numWorkers,
		QueueSize:          numTasks,
		PushRequestTimeout: time.Second,
	}, finder, pusher, metrics, tp.Tracer("test"), nil)
	p.newTicker = func(time.Duration) ticker { return tk }

	p.Start(t.Context())
	tk.tick(t)

	// ждём, пока все 3 воркера одновременно займут задачи
	waitSignal(t, allBusy, "all workers busy")
	close(releaseBatch)

	for range numTasks {
		waitEvent(t, metrics.reqNotify, "task finished")
	}

	p.StopScanner()
	if err := p.WaitWorkers(t.Context()); err != nil {
		t.Fatalf("WaitWorkers() error: %v", err)
	}

	if got := processed.Load(); got != numTasks {
		t.Fatalf("processed=%d, want %d", got, numTasks)
	}
	if got := maxInFlight.Load(); got != numWorkers {
		t.Fatalf("max concurrency=%d, want %d", got, numWorkers)
	}

	// проверяем, что спан воркера связан со спаном сканера через span link, а не как дочерний
	spans := exporter.GetSpans()
	var scanSpanID string
	linkedCount := 0
	for _, s := range spans {
		if s.Name == "positions.scan" {
			scanSpanID = s.SpanContext.SpanID().String()
		}
	}
	if scanSpanID == "" {
		t.Fatal("expected positions.scan span to be recorded")
	}
	for _, s := range spans {
		if s.Name != "positions.request" {
			continue
		}
		if s.Parent.IsValid() {
			t.Fatalf("positions.request must be root span with link, not child of %s", s.Parent.SpanID())
		}
		for _, link := range s.Links {
			if link.SpanContext.SpanID().String() == scanSpanID {
				linkedCount++
			}
		}
	}
	if linkedCount != numTasks {
		t.Fatalf("linked worker spans=%d, want %d", linkedCount, numTasks)
	}
}

// сценарий 6: при отмене контекста сканер и воркеры чисто останавливаются без утечки горутин
func TestPool_StopsCleanlyOnContextCancel(t *testing.T) {
	ctrl := gomock.NewController(t)
	finder := NewMockstaleTripFinder(ctrl)
	pusher := NewMockpushClient(ctrl)
	tk := newFakeTicker()

	p := New(Config{
		CheckInterval:      time.Second,
		StaleAfter:         time.Minute,
		Workers:            4,
		QueueSize:          16,
		PushRequestTimeout: time.Second,
	}, finder, pusher, nil, nil, nil)
	p.newTicker = func(time.Duration) ticker { return tk }

	ctx, cancel := context.WithCancel(t.Context())
	p.Start(ctx)
	cancel()

	if err := p.WaitWorkers(t.Context()); err != nil {
		t.Fatalf("WaitWorkers() error: %v", err)
	}
}

// сценарий 7: ошибка push-сервиса не роняет воркер, следующая задача успешно обрабатывается
func TestPool_PushErrorDoesNotCrashWorker(t *testing.T) {
	ctrl := gomock.NewController(t)
	finder := NewMockstaleTripFinder(ctrl)
	pusher := NewMockpushClient(ctrl)
	metrics := newSyncMetrics()
	tk := newFakeTicker()

	first := model.StaleTrip{TripID: uuid.New(), DriverID: uuid.New()}
	second := model.StaleTrip{TripID: uuid.New(), DriverID: uuid.New()}

	finder.EXPECT().FindStaleActive(gomock.Any(), gomock.Any()).
		Return([]model.StaleTrip{first, second}, nil).Times(1)

	gomock.InOrder(
		pusher.EXPECT().RequestPosition(gomock.Any(), first.TripID, first.DriverID).
			Return(errors.New("push service 503")),
		pusher.EXPECT().RequestPosition(gomock.Any(), second.TripID, second.DriverID).
			Return(nil),
	)

	p := New(Config{
		CheckInterval:      time.Second,
		StaleAfter:         time.Minute,
		Workers:            1,
		QueueSize:          4,
		PushRequestTimeout: time.Second,
	}, finder, pusher, metrics, nil, nil)
	p.newTicker = func(time.Duration) ticker { return tk }

	p.Start(t.Context())
	tk.tick(t)

	firstRes := waitEvent(t, metrics.reqNotify, "first task")
	secondRes := waitEvent(t, metrics.reqNotify, "second task")
	if firstRes != resultError || secondRes != resultSuccess {
		t.Fatalf("got results [%s, %s], want [error, success]", firstRes, secondRes)
	}

	p.StopScanner()
	if err := p.WaitWorkers(t.Context()); err != nil {
		t.Fatalf("WaitWorkers() error: %v", err)
	}
}
