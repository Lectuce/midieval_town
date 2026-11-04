package game

import (
	"cybertown/internal/game/actor"
	"database/sql"
)

type Service struct {
	db     *sql.DB
	Actors *actor.Service
}

func NewService(db *sql.DB) *Service {
	return &Service{
		db:     db,
		Actors: actor.NewService(db),
	}
}
