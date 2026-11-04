package negotiation

const (
	NegotiationStatusInviting   = "inviting"
	NegotiationStatusTravelling = "travelling"
	NegotiationStatusDiscussing = "discussing"
	NegotiationStatusAgreed     = "agreed"
	NegotiationStatusBlocked    = "blocked"
	NegotiationStatusCancelled  = "cancelled"
	NegotiationStatusFailed     = "failed"
)

// DiscussionWork.Kind
const (
	WorkReviewProposal = "review_proposal"
	WorkClarify        = "clarify"
	WorkCounteroffer   = "counteroffer"
	WorkDirector       = "director"
)
