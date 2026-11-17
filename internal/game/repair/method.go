package repair

import (
	"fmt"
	"strings"
	"time"

	"cybertown/internal/game/transport"
)

const (
	MethodReplaceParts = "replace_parts"
	MethodReuseParts   = "reuse_parts"
)

type RepairMethod struct {
	ID                 string `json:"id"`
	Revision           int64  `json:"revision"`
	WorkerNPCID        string `json:"worker_npc_id"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	RequiredCartStatus string `json:"required_cart_status"`
	ResultCartStatus   string `json:"result_cart_status"`
	MaterialCount      int64  `json:"material_count"`
	DurationMinutes    int64  `json:"duration_minutes"`
	MinPriceCoins      int64  `json:"min_price_coins"`
	MaxPriceCoins      int64  `json:"max_price_coins"`
}

// Return fresh values so callers cannot mutate the shared catalog.
func RepairMethods() []RepairMethod {
	return []RepairMethod{
		{ID: MethodReplaceParts, Revision: 1, WorkerNPCID: "blacksmith", Name: "更换损坏部件",
			Description:        "使用替换材料更换损坏部件，耗材较多，修理较快。",
			RequiredCartStatus: transport.CartStatusDamaged, ResultCartStatus: transport.CartStatusReady,
			MaterialCount: 3, DurationMinutes: 60, MinPriceCoins: 4, MaxPriceCoins: 8},
		{ID: MethodReuseParts, Revision: 1, WorkerNPCID: "blacksmith", Name: "修整可用旧件",
			Description:        "修整仍可使用的部件，减少耗材，但需要更多工时。",
			RequiredCartStatus: transport.CartStatusDamaged, ResultCartStatus: transport.CartStatusReady,
			MaterialCount: 2, DurationMinutes: 120, MinPriceCoins: 3, MaxPriceCoins: 6},
	}
}

func ValidateRepairMethods() error {
	seen := make(map[string]bool)
	for _, m := range RepairMethods() {
		if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.Description) == "" || seen[m.ID] {
			return fmt.Errorf("invalid or duplicate repair method %q", m.ID)
		}
		seen[m.ID] = true
		if m.Revision < 1 || m.WorkerNPCID != "blacksmith" || m.RequiredCartStatus != transport.CartStatusDamaged || m.ResultCartStatus != transport.CartStatusReady ||
			m.MaterialCount < 1 || m.DurationMinutes < 1 || m.DurationMinutes > 1440 || m.MinPriceCoins < 0 || m.MaxPriceCoins < m.MinPriceCoins {
			return fmt.Errorf("invalid repair method definition %q", m.ID)
		}
	}
	if len(seen) == 0 {
		return fmt.Errorf("repair method catalog is empty")
	}
	return nil
}

func GetRepairMethod(id string) (RepairMethod, error) {
	for _, m := range RepairMethods() {
		if m.ID == id {
			return m, nil
		}
	}
	return RepairMethod{}, fmt.Errorf("unknown repair method %q", id)
}

// This validates the worker's offer, not the payer's private willingness to pay.
func ValidateRepairMethodTerms(id string, revision, price, materials, minutes int64) error {
	m, err := GetRepairMethod(id)
	if err != nil {
		return err
	}
	if revision != m.Revision {
		return fmt.Errorf("repair method revision conflict")
	}
	if materials != m.MaterialCount || minutes != m.DurationMinutes {
		return fmt.Errorf("repair terms do not match method %q", id)
	}
	if price < m.MinPriceCoins || price > m.MaxPriceCoins {
		return fmt.Errorf("repair price outside method range")
	}
	return nil
}

// start must be authoritative game time, after action availability is checked.
func RepairMethodFinishAt(id string, start time.Time) (time.Time, error) {
	m, err := GetRepairMethod(id)
	if err != nil {
		return time.Time{}, err
	}
	if start.IsZero() {
		return time.Time{}, fmt.Errorf("repair start time is required")
	}
	return start.Add(time.Duration(m.DurationMinutes) * time.Minute), nil
}
