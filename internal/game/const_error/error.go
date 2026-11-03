package constError

import "errors"

var ErrPlayerRevisionConflict = errors.New("player revision conflict")
var ErrPlayerInactive = errors.New("player is not active")

var (
	ErrNegotiationInvalidRequest = errors.New(
		"invalid negotiation request",
	)
	ErrNegotiationRequestConflict = errors.New(
		"negotiation request conflicts with previous request",
	)
	ErrNegotiationUnavailable = errors.New(
		"proposal unavailable for negotiation",
	)
	ErrNegotiationBusy = errors.New(
		"cart or participant is already occupied",
	)

	ErrMeetingNotReady = errors.New("negotiation is not ready for meeting")
)

var (
	ErrQuestNotActive            = errors.New("quest is not active")
	ErrOutsideInvestigationRange = errors.New("player is outside investigation range")
	ErrTransportStateMissing     = errors.New("transport state is missing")
)

var (
	ErrQuestItemsRequired      = errors.New("required quest items are missing")
	ErrOutsideQuestRange       = errors.New("player is outside quest giver range")
	ErrQuestCompletionConflict = errors.New("quest completion conflict")
)

var ErrWorldRevisionConflict = errors.New(
	"world revision conflict",
)
