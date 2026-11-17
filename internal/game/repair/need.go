package repair

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"cybertown/internal/game/storage"
)

const (
	DeadlineNone      = "none"
	DeadlinePreferred = "preferred"
	DeadlineHard      = "hard"
	PriorityCost      = "cost"
	PriorityTime      = "time"
	PriorityBalanced  = "balanced"
)

// RepairNeed is private to the payer until disclosed through a game event.
type RepairNeed struct {
	WorldID             string     `json:"world_id"`
	CartID              string     `json:"cart_id"`
	PayerNPCID          string     `json:"payer_npc_id"`
	Purpose             string     `json:"purpose"`
	NeededBy            *time.Time `json:"needed_by,omitempty"`
	DeadlineKind        string     `json:"deadline_kind"`
	PreferredPriceCoins int64      `json:"preferred_price_coins"`
	MaxPriceCoins       int64      `json:"max_price_coins"`
	Priority            string     `json:"priority"`
	Source              string     `json:"source"`
	Revision            int64      `json:"revision"`
}

func ValidateRepairNeed(n RepairNeed) error {
	for _, value := range []string{n.WorldID, n.CartID, n.PayerNPCID, n.Purpose, n.Source} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("repair need identity, purpose and source are required")
		}
	}
	if n.Revision < 1 || n.PreferredPriceCoins < 0 || n.MaxPriceCoins < n.PreferredPriceCoins {
		return fmt.Errorf("invalid repair need revision or price bounds")
	}
	switch n.Priority {
	case PriorityCost, PriorityTime, PriorityBalanced:
	default:
		return fmt.Errorf("unknown repair priority %q", n.Priority)
	}
	switch n.DeadlineKind {
	case DeadlineNone:
		if n.NeededBy != nil {
			return fmt.Errorf("deadline none must not have needed_by")
		}
	case DeadlinePreferred, DeadlineHard:
		if n.NeededBy == nil || n.NeededBy.IsZero() {
			return fmt.Errorf("deadline requires needed_by")
		}
	default:
		return fmt.Errorf("unknown deadline kind %q", n.DeadlineKind)
	}
	return nil
}

func GetRepairNeed(ctx context.Context, q storage.DBTX, worldID, cartID, payerID string) (RepairNeed, error) {
	var n RepairNeed
	var deadline sql.NullString
	err := q.QueryRowContext(ctx, `SELECT world_id, cart_id, payer_npc_id, purpose,
		needed_by, deadline_kind, preferred_price_coins, max_price_coins, priority, source, revision
		FROM repair_needs WHERE world_id = ? AND cart_id = ? AND payer_npc_id = ?`,
		worldID, cartID, payerID).Scan(&n.WorldID, &n.CartID, &n.PayerNPCID, &n.Purpose,
		&deadline, &n.DeadlineKind, &n.PreferredPriceCoins, &n.MaxPriceCoins, &n.Priority, &n.Source, &n.Revision)
	if err != nil {
		return n, fmt.Errorf("load repair need: %w", err)
	}
	if deadline.Valid {
		value, err := time.Parse(time.RFC3339Nano, deadline.String)
		if err != nil {
			return n, fmt.Errorf("parse repair deadline: %w", err)
		}
		n.NeededBy = &value
	}
	return n, ValidateRepairNeed(n)
}

// InitRepairNeed only seeds missing data; restarting never extends a deadline.
func InitRepairNeed(ctx context.Context, db *sql.DB, initial RepairNeed) (RepairNeed, error) {
	if err := ValidateRepairNeed(initial); err != nil {
		return RepairNeed{}, err
	}
	if initial.Revision != 1 {
		return RepairNeed{}, fmt.Errorf("initial repair need revision must be 1")
	}
	var deadline any
	if initial.NeededBy != nil {
		deadline = initial.NeededBy.UTC().Format(time.RFC3339Nano)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return RepairNeed{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO repair_needs (
		world_id, cart_id, payer_npc_id, purpose, needed_by, deadline_kind,
		preferred_price_coins, max_price_coins, priority, source, revision
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
	ON CONFLICT(world_id, cart_id, payer_npc_id) DO NOTHING`,
		initial.WorldID, initial.CartID, initial.PayerNPCID, initial.Purpose, deadline,
		initial.DeadlineKind, initial.PreferredPriceCoins, initial.MaxPriceCoins, initial.Priority, initial.Source)
	if err != nil {
		return RepairNeed{}, fmt.Errorf("initialize repair need: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return RepairNeed{}, err
	}
	if count > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE worlds SET revision = revision + 1 WHERE id = ?`, initial.WorldID); err != nil {
			return RepairNeed{}, err
		}
	}
	n, err := GetRepairNeed(ctx, tx, initial.WorldID, initial.CartID, initial.PayerNPCID)
	if err != nil {
		return RepairNeed{}, err
	}
	if err := tx.Commit(); err != nil {
		return RepairNeed{}, err
	}
	return n, nil
}
