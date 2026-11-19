//go:build integration

package trip_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	"github.com/aquamarine66/tripgo-course/internal/model"
	addposuc "github.com/aquamarine66/tripgo-course/internal/usecase/trip/addposition"
)

// сценарий 1: выборка устаревших поездок исключает свежие и завершённые и включает поездки без координат
func TestRepository_FindStaleActive(t *testing.T) {
	pool, _, repo := setupIntegrationDeps(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	// 1. самая старая активная поездка без координат — должна быть первой
	staleNoPosOldest := makeSampleTrip(uuid.New())
	staleNoPosOldest.StartedAt = now.Add(-10 * time.Minute)
	cleanupTrip(t, pool, staleNoPosOldest.ID)

	// 2. более свежая активная поездка без координат — должна быть второй
	staleNoPosNewer := makeSampleTrip(uuid.New())
	staleNoPosNewer.StartedAt = now.Add(-5 * time.Minute)
	cleanupTrip(t, pool, staleNoPosNewer.ID)

	// 3. активная поездка со старой последней координатой — должна быть третьей
	staleWithOldPos := makeSampleTrip(uuid.New())
	staleWithOldPos.StartedAt = now.Add(-20 * time.Minute)
	cleanupTrip(t, pool, staleWithOldPos.ID)

	// 4. свежая активная поездка без координат — не должна попасть
	freshNoPos := makeSampleTrip(uuid.New())
	freshNoPos.StartedAt = now.Add(-10 * time.Second)
	cleanupTrip(t, pool, freshNoPos.ID)

	// 5. давно начатая поездка, но со свежей координатой — не должна попасть
	freshWithPos := makeSampleTrip(uuid.New())
	freshWithPos.StartedAt = now.Add(-10 * time.Minute)
	cleanupTrip(t, pool, freshWithPos.ID)

	// 6. завершённая старая поездка — не должна попасть
	completedOld := makeSampleTrip(uuid.New())
	completedOld.StartedAt = now.Add(-10 * time.Minute)
	completedOld.Status = string(domaintrip.StatusCompleted)
	finishedAt := now.Add(-5 * time.Minute)
	completedOld.FinishedAt = &finishedAt
	cleanupTrip(t, pool, completedOld.ID)

	for _, tr := range []*model.Trip{
		staleNoPosOldest, staleNoPosNewer, staleWithOldPos,
		freshNoPos, freshWithPos, completedOld,
	} {
		if err := repo.Save(ctx, tr); err != nil {
			t.Fatalf("Save(%s) error: %v", tr.ID, err)
		}
	}

	if err := repo.UpdateLastPositionAt(ctx, staleWithOldPos.ID, now.Add(-3*time.Minute)); err != nil {
		t.Fatalf("UpdateLastPositionAt(staleWithOldPos) error: %v", err)
	}
	if err := repo.UpdateLastPositionAt(ctx, freshWithPos.ID, now.Add(-10*time.Second)); err != nil {
		t.Fatalf("UpdateLastPositionAt(freshWithPos) error: %v", err)
	}

	got, err := repo.FindStaleActive(ctx, time.Minute)
	if err != nil {
		t.Fatalf("FindStaleActive() error: %v", err)
	}

	indexByID := make(map[uuid.UUID]int, len(got))
	for i, st := range got {
		indexByID[st.TripID] = i
	}

	for _, excluded := range []uuid.UUID{freshNoPos.ID, freshWithPos.ID, completedOld.ID} {
		if _, found := indexByID[excluded]; found {
			t.Fatalf("trip %s should not be in stale selection", excluded)
		}
	}

	idx1, ok1 := indexByID[staleNoPosOldest.ID]
	idx2, ok2 := indexByID[staleNoPosNewer.ID]
	idx3, ok3 := indexByID[staleWithOldPos.ID]
	if !ok1 || !ok2 || !ok3 {
		t.Fatalf("expected all three stale trips in result, found flags: %v, %v, %v", ok1, ok2, ok3)
	}
	if idx1 >= idx2 || idx2 >= idx3 {
		t.Fatalf("expected oldest-first ordering (%d < %d < %d)", idx1, idx2, idx3)
	}
}

// сценарий 8: сохранение координаты двигает last_position_at только вперёд и атомарно откатывается при ошибке
func TestRepository_Positions_UpdatesLastPositionOutOfOrderAndRollsBack(t *testing.T) {
	pool, txm, repo := setupIntegrationDeps(t)
	ctx := context.Background()

	tr := makeSampleTrip(uuid.New())
	cleanupTrip(t, pool, tr.ID)
	if err := repo.Save(ctx, tr); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	uc := addposuc.New(repo, txm, nil, nil)
	now := time.Now().UTC().Truncate(time.Microsecond)
	tNewer := now.Add(-30 * time.Second)
	tOlder := now.Add(-2 * time.Minute)

	// сначала приходит более свежая точка
	if _, err := uc.Execute(ctx, model.CreatePositionInput{
		TripID:     tr.ID,
		Latitude:   59.9350,
		Longitude:  30.3350,
		RecordedAt: tNewer,
	}); err != nil {
		t.Fatalf("Execute(newer) error: %v", err)
	}

	// затем доезжает старая точка из офлайна — last_position_at не должен откатиться назад
	if _, err := uc.Execute(ctx, model.CreatePositionInput{
		TripID:     tr.ID,
		Latitude:   59.9300,
		Longitude:  30.3300,
		RecordedAt: tOlder,
	}); err != nil {
		t.Fatalf("Execute(older) error: %v", err)
	}

	updated, err := repo.GetByID(ctx, tr.ID)
	if err != nil {
		t.Fatalf("GetByID() error: %v", err)
	}
	if updated.LastPositionAt == nil || !updated.LastPositionAt.Equal(tNewer) {
		t.Fatalf("LastPositionAt=%v, want %v (older point must not move it backwards)", updated.LastPositionAt, tNewer)
	}

	route, err := repo.ListPositions(ctx, tr.ID)
	if err != nil {
		t.Fatalf("ListPositions() error: %v", err)
	}
	if len(route) != 2 || !route[0].RecordedAt.Equal(tOlder) || !route[1].RecordedAt.Equal(tNewer) {
		t.Fatalf("unexpected route ordering: %+v", route)
	}

	// проверяем атомарный откат: ошибка после вставки и обновления откатывает оба изменения
	tFailed := now.Add(-5 * time.Second)
	boom := errors.New("simulated failure after writes")
	err = txm.Do(ctx, func(txCtx context.Context) error {
		pos := &model.TripPosition{
			TripID:     tr.ID,
			Latitude:   59.9400,
			Longitude:  30.3400,
			RecordedAt: tFailed,
		}
		if err := repo.SavePosition(txCtx, pos); err != nil {
			return err
		}
		if err := repo.UpdateLastPositionAt(txCtx, tr.ID, tFailed); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected simulated error, got %v", err)
	}

	afterRollback, err := repo.GetByID(ctx, tr.ID)
	if err != nil {
		t.Fatalf("GetByID() after rollback error: %v", err)
	}
	if afterRollback.LastPositionAt == nil || !afterRollback.LastPositionAt.Equal(tNewer) {
		t.Fatalf("LastPositionAt after rollback=%v, want %v", afterRollback.LastPositionAt, tNewer)
	}

	routeAfterRollback, err := repo.ListPositions(ctx, tr.ID)
	if err != nil {
		t.Fatalf("ListPositions() after rollback error: %v", err)
	}
	if len(routeAfterRollback) != 2 {
		t.Fatalf("expected 2 positions after rollback, got %d", len(routeAfterRollback))
	}
}
