package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

// интеграционные тесты менеджера транзакций. Нужна бд с накатанными
// миграциями: TEST_DATABASE_URL=$DATABASE_URL go test ./internal/repository/
// без TEST_DATABASE_URL тесты пропускаются.
func newTestDeps(t *testing.T) (*pgxpool.Pool, *TxManager, *TripRepository) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool, NewTxManager(pool), NewTripRepository(pool, 3*time.Second)
}

func newTrip() *model.Trip {
	return &model.Trip{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		DriverID:  uuid.New(),
		Status:    model.StatusActive,
		Price:     100,
		StartLat:  59.9,
		StartLng:  30.3,
		EndLat:    59.92,
		EndLng:    30.36,
		StartedAt: time.Now().UTC(),
	}
}

func assertTripMissing(t *testing.T, repo *TripRepository, id uuid.UUID) {
	t.Helper()
	if _, err := repo.GetTripByID(context.Background(), id); !errors.Is(err, model.ErrTripNotFound) {
		t.Fatalf("expected trip to be rolled back, got err=%v", err)
	}
}

func TestDo_RollbackOnError(t *testing.T) {
	_, txm, repo := newTestDeps(t)
	trip := newTrip()
	errBoom := errors.New("boom")

	err := txm.Do(context.Background(), func(ctx context.Context) error {
		if err := repo.CreateTrip(ctx, trip); err != nil {
			return err
		}
		return errBoom // «падение второй вставки»
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
	assertTripMissing(t, repo, trip.ID)
}

func TestDo_RollbackOnPanic(t *testing.T) {
	_, txm, repo := newTestDeps(t)
	trip := newTrip()

	func() {
		defer func() {
			if p := recover(); p == nil {
				t.Fatal("expected panic to be re-raised")
			}
		}()
		_ = txm.Do(context.Background(), func(ctx context.Context) error {
			if err := repo.CreateTrip(ctx, trip); err != nil {
				return err
			}
			panic("boom")
		})
	}()

	assertTripMissing(t, repo, trip.ID)
}

func TestDo_NestedReusesTransaction(t *testing.T) {
	_, txm, repo := newTestDeps(t)
	trip := newTrip()
	errBoom := errors.New("boom")

	err := txm.Do(context.Background(), func(outer context.Context) error {
		outerTx := outer.Value(txKey{}).(pgx.Tx)

		if err := txm.Do(outer, func(inner context.Context) error {
			if inner.Value(txKey{}).(pgx.Tx) != outerTx {
				t.Error("nested Do opened a second transaction")
			}
			return repo.CreateTrip(inner, trip)
		}); err != nil {
			return err
		}

		// вложенный Do ничего не закоммитил: откат внешнего откатывает и его
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}
	assertTripMissing(t, repo, trip.ID)
}

func TestCreateTrip_AtomicWithHistory(t *testing.T) {
	_, txm, repo := newTestDeps(t)
	trip := newTrip()

	err := txm.Do(context.Background(), func(ctx context.Context) error {
		if err := repo.CreateTrip(ctx, trip); err != nil {
			return err
		}
		// to_status нарушает CHECK — вторая вставка падает в бд
		return repo.AddStatusHistory(ctx, model.StatusChange{
			TripID:    trip.ID,
			ToStatus:  "broken",
			ChangedAt: time.Now().UTC(),
		})
	})
	if err == nil {
		t.Fatal("expected history insert to fail")
	}
	assertTripMissing(t, repo, trip.ID)
}
