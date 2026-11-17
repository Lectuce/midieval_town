package repair

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cybertown/internal/game/storage"
)

func TestRepairNeedUpgradeAndRestartPreserveSave(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "game.db")
	db, err := storage.OpenDatabase(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-feature save without the new table.
	_, err = db.ExecContext(ctx, `DROP TABLE repair_needs;
	INSERT INTO worlds VALUES ('town-1', '1348-05-01T08:00:00Z', 1);
	INSERT INTO scenes VALUES ('town-1', 'town', 'Town');
	INSERT INTO npcs VALUES ('town-1', 'merchant', 'Martin', 'merchant', 'p', 'b', 'town', 'idle', 0, 0, 0, 1);
	INSERT INTO npcs VALUES ('town-1', 'blacksmith', 'Roland', 'blacksmith', 'p', 'b', 'town', 'idle', 0, 0, 0, 1);
	INSERT INTO carts VALUES ('town-1', 'abandoned_cart', 'town', 0, 0, 0, 'damaged', 100, 1);
	INSERT INTO repair_proposals (world_id,id,cart_id,payer_npc_id,worker_npc_id,price_coins,material_count,duration_minutes)
	VALUES ('town-1','legacy','abandoned_cart','merchant','blacksmith',0,3,120);`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	db, err = storage.OpenDatabase(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	deadline := time.Date(1348, 5, 1, 12, 0, 0, 0, time.UTC)
	n := RepairNeed{WorldID: "town-1", CartID: "abandoned_cart", PayerNPCID: "merchant", Purpose: "Recover cargo", NeededBy: &deadline,
		DeadlineKind: DeadlinePreferred, PreferredPriceCoins: 5, MaxPriceCoins: 10, Priority: PriorityCost, Source: "initial", Revision: 1}
	if _, err := InitRepairNeed(ctx, db, n); err != nil {
		t.Fatal(err)
	}
	// New startup defaults must not overwrite the saved requirement.
	later := deadline.Add(24 * time.Hour)
	n.NeededBy = &later
	n.MaxPriceCoins = 20
	got, err := InitRepairNeed(ctx, db, n)
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxPriceCoins != 10 || !got.NeededBy.Equal(deadline) || got.Revision != 1 {
		t.Fatalf("save overwritten: %+v", got)
	}
	var worldRevision, price int64
	if err := db.QueryRowContext(ctx, `SELECT revision FROM worlds WHERE id='town-1'`).Scan(&worldRevision); err != nil {
		t.Fatal(err)
	}
	if worldRevision != 2 {
		t.Fatalf("duplicate initialization changed world: %d", worldRevision)
	}
	if err := db.QueryRowContext(ctx, `SELECT price_coins FROM repair_proposals WHERE id='legacy'`).Scan(&price); err != nil {
		t.Fatal(err)
	}
	if price != 0 {
		t.Fatal("legacy offer changed")
	}
}

func TestRepairMethodRejectsInventedTerms(t *testing.T) {
	if err := ValidateRepairMethods(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id                                  string
		revision, price, materials, minutes int64
	}{
		{"unknown", 1, 5, 3, 60}, {MethodReplaceParts, 2, 5, 3, 60},
		{MethodReplaceParts, 1, 5, 1, 1}, {MethodReplaceParts, 1, 100, 3, 60},
	} {
		if err := ValidateRepairMethodTerms(c.id, c.revision, c.price, c.materials, c.minutes); err == nil {
			t.Fatalf("accepted invalid terms: %+v", c)
		}
	}
	if err := ValidateRepairMethodTerms(MethodReplaceParts, 1, 5, 3, 60); err != nil {
		t.Fatal(err)
	}
	start := time.Date(1348, 5, 1, 8, 0, 0, 0, time.UTC)
	finish, err := RepairMethodFinishAt(MethodReuseParts, start)
	if err != nil || !finish.Equal(start.Add(2*time.Hour)) {
		t.Fatalf("invalid finish: %v %v", finish, err)
	}
}
