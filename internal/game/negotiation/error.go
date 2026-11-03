package negotiation

import "errors"

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
