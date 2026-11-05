package trip

import (
	"github.com/jackc/pgx/v5"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

func scanTrip(row pgx.Row) (*model.Trip, error) {
	var t model.Trip
	err := row.Scan(
		&t.ID, &t.UserID, &t.DriverID, &t.Status, &t.Price,
		&t.StartLat, &t.StartLng, &t.EndLat, &t.EndLng,
		&t.StartedAt, &t.FinishedAt, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
