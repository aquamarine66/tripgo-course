//go:build integration

package trip_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	"github.com/aquamarine66/tripgo-course/internal/infra/postgres/transaction"
	"github.com/aquamarine66/tripgo-course/internal/model"
	triprepo "github.com/aquamarine66/tripgo-course/internal/repository/trip"
	finishuc "github.com/aquamarine66/tripgo-course/internal/usecase/trip/finish"
)

func setupIntegrationDeps(t *testing.T) (*pgxpool.Pool, *transaction.Manager, *triprepo.Repository) {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("DATABASE_URL is not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool, transaction.NewManager(pool), triprepo.New(pool, 3*time.Second)
}

func makeSampleTrip(driverID uuid.UUID) *model.Trip {
	return &model.Trip{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		DriverID:  driverID,
		Status:    string(domaintrip.StatusActive),
		Price:     1450,
		StartLat:  59.9398,
		StartLng:  30.3146,
		EndLat:    59.9290,
		EndLng:    30.3626,
		StartedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
}

func cleanupTrip(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM trips WHERE id = $1`, id)
	})
}

func TestRepository_SaveAndGetByID(t *testing.T) {
	pool, _, repo := setupIntegrationDeps(t)

	tr := makeSampleTrip(uuid.New())
	cleanupTrip(t, pool, tr.ID)

	if err := repo.Save(context.Background(), tr); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	got, err := repo.GetByID(context.Background(), tr.ID)
	if err != nil {
		t.Fatalf("GetByID() error: %v", err)
	}
	if got.ID != tr.ID || got.DriverID != tr.DriverID || got.Price != tr.Price {
		t.Fatalf("got %+v, want %+v", got, tr)
	}
}

func TestRepository_Save_DuplicateActiveDriverReturnsErrDriverBusy(t *testing.T) {
	pool, _, repo := setupIntegrationDeps(t)

	driverID := uuid.New()
	first := makeSampleTrip(driverID)
	second := makeSampleTrip(driverID)
	cleanupTrip(t, pool, first.ID)
	cleanupTrip(t, pool, second.ID)

	if err := repo.Save(context.Background(), first); err != nil {
		t.Fatalf("first Save() error: %v", err)
	}

	err := repo.Save(context.Background(), second)
	if !errors.Is(err, domaintrip.ErrDriverBusy) {
		t.Fatalf("expected ErrDriverBusy on second active trip for same driver, got %v", err)
	}
}

func TestRepository_TransactionRollbackOnSecondInsertFailure(t *testing.T) {
	pool, txm, repo := setupIntegrationDeps(t)

	tr := makeSampleTrip(uuid.New())
	cleanupTrip(t, pool, tr.ID)

	err := txm.Do(context.Background(), func(ctx context.Context) error {
		if err := repo.Save(ctx, tr); err != nil {
			return err
		}
		// Нарушаем CHECK (to_status IN ('active', 'completed')) во второй таблице
		return repo.AddStatusHistory(ctx, model.StatusChange{
			TripID:    tr.ID,
			ToStatus:  "invalid_status",
			Reason:    "should fail",
			ChangedAt: time.Now().UTC(),
		})
	})
	if err == nil {
		t.Fatal("expected second insert into trip_status_history to fail")
	}

	_, getErr := repo.GetByID(context.Background(), tr.ID)
	if !errors.Is(getErr, domaintrip.ErrTripNotFound) {
		t.Fatalf("expected first insert into trips to be rolled back, got err=%v", getErr)
	}
}

func TestRepository_TransactionRollbackOnPanic(t *testing.T) {
	pool, txm, repo := setupIntegrationDeps(t)

	tr := makeSampleTrip(uuid.New())
	cleanupTrip(t, pool, tr.ID)

	func() {
		defer func() {
			if p := recover(); p == nil {
				t.Fatal("expected panic to be re-thrown")
			}
		}()

		_ = txm.Do(context.Background(), func(ctx context.Context) error {
			if err := repo.Save(ctx, tr); err != nil {
				return err
			}
			panic("unexpected crash inside transaction")
		})
	}()

	_, getErr := repo.GetByID(context.Background(), tr.ID)
	if !errors.Is(getErr, domaintrip.ErrTripNotFound) {
		t.Fatalf("expected trip to be rolled back on panic, got err=%v", getErr)
	}
}

func TestRepository_ConcurrentFinishAllowsOnlyOneSuccess(t *testing.T) {
	pool, txm, repo := setupIntegrationDeps(t)

	tr := makeSampleTrip(uuid.New())
	cleanupTrip(t, pool, tr.ID)

	if err := repo.Save(context.Background(), tr); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	uc := finishuc.New(repo, txm, nil, nil)

	const workers = 10
	var (
		wg        sync.WaitGroup
		successes atomic.Int32
		conflicts atomic.Int32
	)

	startGate := make(chan struct{})
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startGate
			_, err := uc.Execute(context.Background(), tr.ID)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, domaintrip.ErrTripCompleted):
				conflicts.Add(1)
			default:
				t.Errorf("unexpected error during concurrent finish: %v", err)
			}
		}()
	}

	close(startGate)
	wg.Wait()

	if successes.Load() != 1 || conflicts.Load() != workers-1 {
		t.Fatalf("expected 1 success and %d conflicts, got %d successes and %d conflicts",
			workers-1, successes.Load(), conflicts.Load())
	}
}
