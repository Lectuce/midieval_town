package game

import (
	"context"
	"database/sql"
)

type NegotiationMeetingRoute struct {
	ID              string     `json:"id"`
	SceneID         string     `json:"scene_id"`
	Floor           int        `json:"floor"`
	Speed           float64    `json:"speed"`
	MerchantPoints  []Position `json:"merchant_points"`
	BlacksmithStand Position   `json:"blacksmith_stand"`
}

func StartNegotiationMeeting(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	negotiationID string,
	route NegotiationMeetingRoute,
) error {

}
