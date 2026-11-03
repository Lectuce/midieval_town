package repair

import "errors"

var (
	ErrRepairProposalRevisionConflict = errors.New(
		"repair proposal revision conflict",
	)
	ErrRepairProposalNotConfirmable = errors.New(
		"repair proposal cannot be confirmed",
	)
	ErrRepairProposalParticipant = errors.New(
		"NPC is not a participant in this repair proposal",
	)

	ErrRepairProposalDeliveryDenied = errors.New(
		"repair proposal cannot be delivered",
	)
)
