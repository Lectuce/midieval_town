package quest

import "errors"

var (
	ErrQuestNotActive            = errors.New("quest is not active")
	ErrOutsideInvestigationRange = errors.New("player is outside investigation range")
	ErrTransportStateMissing     = errors.New("transport state is missing")
)

var (
	ErrPlayerInactive          = errors.New("player is not active")
	ErrQuestItemsRequired      = errors.New("required quest items are missing")
	ErrOutsideQuestRange       = errors.New("player is outside quest giver range")
	ErrQuestCompletionConflict = errors.New("quest completion conflict")
)
