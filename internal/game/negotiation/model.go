package negotiation

import "cybertown/internal/game/model"

type Negotiation struct {
	ID                      string
	WorldID                 string
	PlayerID                string
	CurrentProposalID       string
	Status                  string
	NextActorID             string
	RoundCount              int
	DirectorCalls           int
	Revision                int64
	CartID                  string
	RequestID               string
	InitialProposalID       string
	InitialProposalRevision int64
	CurrentProposalRevision int64
	CreatedAt               int64
}

type NegotiationParticipant struct {
	NegotiationID string
	NPCID         string
	InviteStatus  string // pending、accepted、declined
	ArrivalStatus string // not_started、travelling、arrived
}

type NegotiationEvent struct {
	ID            string
	NegotiationID string
	ActorID       string
	RecipientID   string
	Type          string
	ProposalID    string
	Text          string
}

// 后台领取的一条邀请任务。
type NegotiationInvitation struct {
	Negotiation Negotiation
	EventID     string
	NPCID       string
	Text        string

	// 本次模型读取的角色版本，保存决定时再次检查。
	NPCRevision int64
}

type InvitationDecision struct {
	Action string `json:"action"`
	Speech string `json:"speech"`
}

type NegotiationMeetingRoute struct {
	ID              string           `json:"id"`
	SceneID         string           `json:"scene_id"`
	Floor           int              `json:"floor"`
	Speed           float64          `json:"speed"`
	MerchantPoints  []model.Position `json:"merchant_points"`
	BlacksmithStand model.Position   `json:"blacksmith_stand"`
}

type meetingMoveJob struct {
	ID                  string
	NegotiationID       string
	NPCID               string
	SceneID             string
	Floor               int
	Speed               float64
	StartedGameTime     string
	RouteJSON           string
	ExpectedNPCRevision int64
	Revision            int64
}

type CreateNegotiationRequest struct {
	RequestID        string
	ProposalID       string
	ExpectedRevision int64
}

// 这里只寻找候选会话，真正能否出发仍由
// StartNegotiationMeeting 在事务中重新检查。
type ReadyMeeting struct {
	WorldID  string
	ID       string
	Revision int64
}

type RepairTerms struct {
	PriceCoins      int64 `json:"price_coins"`
	MaterialCount   int64 `json:"material_count"`
	DurationMinutes int64 `json:"duration_minutes"`
}

type DiscussionDecision struct {
	Action           string       `json:"action"`
	ProposalID       string       `json:"proposal_id"`
	ExpectedRevision int64        `json:"expected_revision"`
	Speech           string       `json:"speech"`
	Topic            string       `json:"topic,omitempty"`
	Issue            string       `json:"issue,omitempty"`
	Terms            *RepairTerms `json:"terms,omitempty"`
}

type DiscussionPlan struct {
	Action        string `json:"action"`
	ActorID       string `json:"actor_id"`
	Topic         string `json:"topic"`
	SourceEventID string `json:"source_event_id"`
	Reason        string `json:"reason"`
}

type DiscussionWork struct {
	WorldID             string
	NegotiationID       string
	EventID             string
	SourceEventID       string
	ActorID             string
	Kind                string
	Topic               string
	ProposalID          string
	ProposalRevision    int64
	NegotiationRevision int64
	Attempt             int
}
