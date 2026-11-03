package game

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

const (
	NegotiationStatusInviting   = "inviting"
	NegotiationStatusTravelling = "travelling"
	NegotiationStatusDiscussing = "discussing"
	NegotiationStatusAgreed     = "agreed"
	NegotiationStatusBlocked    = "blocked"
	NegotiationStatusCancelled  = "cancelled"
	NegotiationStatusFailed     = "failed"
)

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
