package negotiation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"cybertown/internal/game/repair"
)

var (
	ErrDiscussionState = errors.New("会面、角色或报价状态已经变化")
	ErrDiscussionInput = errors.New("协商输入不合法")
)

const (
	DiscussionReview   = "review"
	DiscussionClarify  = "clarify"
	DiscussionCounter  = "counter"
	DiscussionDirector = "director"

	MaxDiscussionReviews = 3
	MaxDiscussionCalls   = 8
	MaxDirectorCalls     = 2
)

type DiscussionTerms struct {
	PriceCoins      int64 `json:"price_coins"`
	MaterialCount   int64 `json:"material_count"`
	DurationMinutes int64 `json:"duration_minutes"`
}

type DiscussionDecision struct {
	Action string `json:"action"`
	Speech string `json:"speech"`
	Topic  string `json:"topic"`
	Reason string `json:"reason"`
	Target string `json:"target"`

	Terms *DiscussionTerms `json:"terms"`
}

type DiscussionSelf struct {
	Name       string `json:"name"`
	Persona    string `json:"persona"`
	Background string `json:"background"`

	Coins     int64 `json:"coins"`
	Materials int64 `json:"materials"`

	Revision         int64 `json:"-"`
	CoinsRevision    int64 `json:"-"`
	MaterialRevision int64 `json:"-"`
}

type DiscussionLine struct {
	Seq    int64  `json:"seq"`
	Actor  string `json:"actor"`
	Speech string `json:"speech"`
	Action string `json:"action,omitempty"`
	Topic  string `json:"topic,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type DiscussionInput struct {
	Kind    string `json:"kind"`
	ActorID string `json:"actor_id"`
	Topic   string `json:"topic"`

	Proposal repair.RepairProposal `json:"proposal"`
	Self     *DiscussionSelf       `json:"self,omitempty"`
	History  []DiscussionLine      `json:"history"`

	AllowedActions   []string `json:"allowed_actions"`
	RemainingReviews int      `json:"remaining_reviews"`
	RemainingCalls   int      `json:"remaining_calls"`
}

type DiscussionWork struct {
	Negotiation Negotiation
	EventID     string
	SourceID    string
	Input       DiscussionInput
}

// 存在数据库中的结构化结果。
// Private 的内容不会从会面接口返回给玩家。
type discussionPayload struct {
	Decision *DiscussionDecision `json:"decision,omitempty"`
}

func InitDiscussion(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS negotiation_discussion_data (
			world_id TEXT NOT NULL,
			event_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			topic TEXT NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL DEFAULT '{}',
			public INTEGER NOT NULL DEFAULT 0 CHECK (public IN (0, 1)),

			PRIMARY KEY (world_id, event_id),
			FOREIGN KEY (world_id, event_id)
				REFERENCES negotiation_events(world_id, id)
		);
	`)
	return err
}

func AllowedDiscussionActions(kind, actorID string) []string {
	switch kind {
	case DiscussionReview:
		if actorID == "merchant" {
			return []string{"accept_proposal", "request_change", "withdraw"}
		}
	case DiscussionClarify:
		if actorID == "merchant" || actorID == "blacksmith" {
			return []string{"clarify", "withdraw"}
		}
	case DiscussionCounter:
		if actorID == "blacksmith" {
			return []string{"counteroffer", "clarify", "withdraw"}
		}
	case DiscussionDirector:
		return []string{"ask_clarification", "request_counteroffer", "end"}
	}
	return nil
}

func DecodeDiscussionDecision(text string) (DiscussionDecision, error) {
	var d DiscussionDecision
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&d); err != nil {
		return d, err
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return d, fmt.Errorf("unexpected content after JSON")
	}

	d.Speech = strings.TrimSpace(d.Speech)
	d.Reason = strings.TrimSpace(d.Reason)
	return d, nil
}

func discussionTextOK(text string) bool {
	return strings.TrimSpace(text) != "" &&
		utf8.ValidString(text) &&
		utf8.RuneCountInString(text) <= 256
}

func discussionTopicOK(topic string) bool {
	return topic == "price" || topic == "materials" || topic == "duration"
}

func ValidateDiscussionDecision(
	input DiscussionInput,
	d DiscussionDecision,
) error {
	allowed := false
	for _, action := range AllowedDiscussionActions(input.Kind, input.ActorID) {
		if d.Action == action {
			allowed = true
			break
		}
	}
	if !allowed {
		return ErrDiscussionInput
	}

	if input.Kind == DiscussionDirector {
		if !discussionTextOK(d.Reason) || d.Speech != "" || d.Terms != nil {
			return ErrDiscussionInput
		}
		if d.Action == "end" {
			if d.Target != "" || d.Topic != "" {
				return ErrDiscussionInput
			}
			return nil
		}
		if !discussionTopicOK(d.Topic) {
			return ErrDiscussionInput
		}
		if d.Target != "merchant" && d.Target != "blacksmith" {
			return ErrDiscussionInput
		}
		if d.Action == "request_counteroffer" && d.Target != "blacksmith" {
			return ErrDiscussionInput
		}
		return nil
	}

	if !discussionTextOK(d.Speech) || d.Target != "" {
		return ErrDiscussionInput
	}

	switch d.Action {
	case "request_change", "clarify":
		if !discussionTopicOK(d.Topic) || !discussionTextOK(d.Reason) {
			return ErrDiscussionInput
		}
	case "counteroffer":
		if d.Terms == nil ||
			d.Terms.PriceCoins < 0 ||
			d.Terms.MaterialCount < 1 ||
			d.Terms.DurationMinutes < 1 ||
			d.Terms.DurationMinutes > 1440 {
			return ErrDiscussionInput
		}
	case "accept_proposal", "withdraw":
		if d.Topic != "" || d.Reason != "" {
			return ErrDiscussionInput
		}
	}

	if d.Action != "counteroffer" && d.Terms != nil {
		return ErrDiscussionInput
	}
	return nil
}
