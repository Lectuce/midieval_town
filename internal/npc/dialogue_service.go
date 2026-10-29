package npc

import (
	"context"
	"cybertown/internal/game"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/anthropics/anthropic-sdk-go"
)

var (
	ErrInvalidMessage  = errors.New("invalid message")
	ErrInvalidRequest  = errors.New("invalid chat request")
	ErrDialogueBusy    = errors.New("dialogue is busy")
	ErrRequestConflict = errors.New("request id reused with different message")
	ErrPlayerInactive  = errors.New("player is not active")
)

// 玩家这次要和谁对话
type ChatRequest struct {
	WorldID   string
	PlayerID  string
	NPCID     string
	RequestID string // 这一次提交的唯一编号
	Message   string
}

type ChatResponse struct {
	NPCID           string                    `json:"npc_id"`
	RequestID       string                    `json:"request_id"`
	Reply           string                    `json:"reply"`
	RepairProposals []game.RepairProposalCard `json:"repair_proposals"`
}

// 哪一组对话
type conversationKey struct {
	WorldID  string
	PlayerID string
	NPCID    string
}

// 处理对话的对象
type DialogueService struct {
	db    *sql.DB
	agent *Agent

	// 同一个会话同时只允许一个请求。
	conversations sync.Map
}

func NewDialogueService(ctx context.Context, db *sql.DB, agent *Agent) (*DialogueService, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	if agent == nil || agent.Engine == nil {
		return nil, fmt.Errorf("NPC agent is required")
	}

	// 一条记录代表一次成功完成的玩家输入和 NPC 回复，
	// messages_json 同时保留该轮的工具调用及工具结果
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS npc_dialogue_turns (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			world_id TEXT NOT NULL,
			player_id TEXT NOT NULL,
			npc_id TEXT NOT NULL,
			request_id TEXT NOT NULL,
			input TEXT NOT NULL,
			reply TEXT NOT NULL,
			messages_json TEXT NOT NULL,

			UNIQUE (world_id, player_id, npc_id, request_id),

			FOREIGN KEY (world_id, player_id)
				REFERENCES players(world_id, id),

			FOREIGN KEY (world_id, npc_id)
				REFERENCES npcs(world_id, id)
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("create dialogue table: %w", err)
	}

	return &DialogueService{
		db:    db,
		agent: agent,
	}, nil
}

func (s *DialogueService) Chat(
	ctx context.Context,
	req ChatRequest,
	onText func(string) error,
) (ChatResponse, error) {
	if req.WorldID == "" || req.PlayerID == "" ||
		req.NPCID == "" || req.RequestID == "" ||
		len(req.RequestID) > 128 {
		return ChatResponse{}, ErrInvalidRequest
	}

	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" ||
		!utf8.ValidString(req.Message) ||
		utf8.RuneCountInString(req.Message) > 256 {
		return ChatResponse{}, ErrInvalidMessage
	}

	player, err := game.GetPlayer(
		ctx, s.db, req.WorldID, req.PlayerID,
	)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("load player: %w", err)
	}

	character, err := game.GetNPC(
		ctx, s.db, req.WorldID, req.NPCID,
	)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("load NPC: %w", err)
	}

	key := conversationKey{
		WorldID:  req.WorldID,
		PlayerID: req.PlayerID,
		NPCID:    req.NPCID,
	}

	value, _ := s.conversations.LoadOrStore(key, make(chan struct{}, 1))

	gate := value.(chan struct{})

	// 同一个玩家与同一个 NPC 的对话，上一条消息还没处理完，就拒绝新的请求
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	default:
		return ChatResponse{}, ErrDialogueBusy
	}

	response := ChatResponse{
		NPCID:     character.ID,
		RequestID: req.RequestID,
	}

	// 已经成功保存的请求直接复用，避免重复调用模型。
	var previousInput string
	err = s.db.QueryRowContext(ctx, `
		SELECT input, reply
		FROM npc_dialogue_turns
		WHERE world_id = ?
			AND player_id = ?
			AND npc_id = ?
			AND request_id = ?
	`,
		req.WorldID,
		req.PlayerID,
		req.NPCID,
		req.RequestID,
	).Scan(&previousInput, &response.Reply)

	if err == nil {
		if previousInput != req.Message {
			return ChatResponse{}, ErrRequestConflict
		}

		response.RepairProposals, err = game.GetPlayerRepairProposals(
			ctx, s.db, req.WorldID, req.PlayerID,
		)
		if err != nil {
			return ChatResponse{}, fmt.Errorf(
				"load repair proposal cards: %w", err,
			)
		}

		return response, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ChatResponse{}, fmt.Errorf("load previous reply: %w", err)
	}

	if player.Status != "active" {
		return ChatResponse{}, ErrPlayerInactive
	}

	// 场景、楼层和交互距离校验。

	rows, err := s.db.QueryContext(ctx, `
		SELECT messages_json
		FROM npc_dialogue_turns
		WHERE world_id = ?
			AND player_id = ?
			AND npc_id = ?
		ORDER BY id DESC
		LIMIT 8
	`,
		req.WorldID,
		req.PlayerID,
		req.NPCID,
	)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("load dialogue history: %w", err)
	}

	var turns [][]anthropic.MessageParam

	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return ChatResponse{}, fmt.Errorf("scan dialogue history: %w", err)
		}

		var messages []anthropic.MessageParam
		if err := json.Unmarshal([]byte(raw), &messages); err != nil {
			rows.Close()
			return ChatResponse{}, fmt.Errorf("decode dialogue history: %w", err)
		}

		turns = append(turns, messages)
	}

	rowsErr := rows.Err()
	rows.Close()

	if rowsErr != nil {
		return ChatResponse{}, fmt.Errorf("read dialogue history: %w", rowsErr)
	}

	// SQL 查到的是从新到旧，传给模型时恢复成从旧到新。
	var history []anthropic.MessageParam
	for i := len(turns) - 1; i >= 0; i-- {
		history = append(history, turns[i]...)
	}

	facts, err := game.GetKnownFacts(ctx, s.db, character.WorldID, character.ID)
	if err != nil {
		return ChatResponse{}, fmt.Errorf(
			"load NPC knowledge: %v", err,
		)
	}

	repairFacts, err := game.GetRepairCoordinationFacts(
		ctx,
		s.db,
		character.WorldID,
		character.ID,
	)
	if err != nil {
		return ChatResponse{}, fmt.Errorf(
			"load repair coordination messages: %w",
			err,
		)
	}

	facts = append(facts, repairFacts...)

	result, err := s.agent.Reply(ctx, character, player, facts, history, req, onText)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("NPC reply: %w", err)
	}

	response.Reply = strings.TrimSpace(result.Text)
	if response.Reply == "" {
		return ChatResponse{}, fmt.Errorf("NPC returned an empty reply")
	}

	// 当前 Engine 返回：传入的历史 + 本轮新增消息。
	// 只保存本轮新增部分，避免每轮重复保存之前的全部历史。
	if len(result.Messages) <= len(history) {
		return ChatResponse{}, fmt.Errorf("NPC returned no new messages")
	}

	raw, err := json.Marshal(result.Messages[len(history):])
	if err != nil {
		return ChatResponse{}, fmt.Errorf("encode dialogue turn: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO npc_dialogue_turns (
			world_id, player_id, npc_id,
			request_id, input, reply, messages_json
		)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		req.WorldID,
		req.PlayerID,
		req.NPCID,
		req.RequestID,
		req.Message,
		response.Reply,
		string(raw),
	)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("save dialogue turn: %w", err)
	}

	// 保存对话成功后，读取玩家已经取得的报价。
	response.RepairProposals, err = game.GetPlayerRepairProposals(
		ctx,
		s.db,
		req.WorldID,
		req.PlayerID,
	)
	if err != nil {
		return ChatResponse{}, fmt.Errorf(
			"load repair proposal cards: %w",
			err,
		)
	}

	return response, nil
}
