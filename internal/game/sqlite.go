package game

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS worlds (
	id TEXT PRIMARY KEY NOT NULL,
	game_time TEXT NOT NULL,
	revision INTEGER NOT NULL CHECK (revision >= 1)
);

CREATE TABLE IF NOT EXISTS scenes (
	world_id TEXT NOT NULL,
	id TEXT NOT NULL,
	name TEXT NOT NULL,

	PRIMARY KEY (world_id, id),
	FOREIGN KEY (world_id) REFERENCES worlds(id)
);

CREATE TABLE IF NOT EXISTS players (
	world_id TEXT NOT NULL,
	id TEXT NOT NULL,
	name TEXT NOT NULL,
	scene_id TEXT NOT NULL,
	status TEXT NOT NULL,
	position_x REAL NOT NULL,
	position_y REAL NOT NULL,
	floor INTEGER NOT NULL CHECK (floor BETWEEN 0 AND 3),
	revision INTEGER NOT NULL CHECK (revision >= 1),

	PRIMARY KEY (world_id, id),
	FOREIGN KEY (world_id, scene_id)
		REFERENCES scenes(world_id, id)
);

CREATE TABLE IF NOT EXISTS npcs (
	world_id TEXT NOT NULL,
	id TEXT NOT NULL,
	name TEXT NOT NULL,
	role TEXT NOT NULL,
	persona TEXT NOT NULL,
	background TEXT NOT NULL,
	scene_id TEXT NOT NULL,
	current_activity TEXT NOT NULL,
	position_x REAL NOT NULL,
	position_y REAL NOT NULL,
	floor INTEGER NOT NULL CHECK (floor BETWEEN 0 AND 3),
	revision INTEGER NOT NULL CHECK (revision >= 1),

	PRIMARY KEY (world_id, id),
	FOREIGN KEY (world_id, scene_id)
		REFERENCES scenes(world_id, id)
);

CREATE TABLE IF NOT EXISTS player_quests (
	world_id TEXT NOT NULL,
	player_id TEXT NOT NULL,
	quest_id TEXT NOT NULL,
	status TEXT NOT NULL
		CHECK (status IN ('active', 'completed')),
	revision INTEGER NOT NULL DEFAULT 1
		CHECK (revision >= 1),

	PRIMARY KEY (world_id, player_id, quest_id),

	FOREIGN KEY (world_id, player_id)
		REFERENCES players(world_id, id)
);

CREATE TABLE IF NOT EXISTS player_evidence (
    world_id TEXT NOT NULL,
    player_id TEXT NOT NULL,
    evidence_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    discovered_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (world_id, player_id, evidence_id),

    FOREIGN KEY (world_id, player_id)
        REFERENCES players(world_id, id)
);

CREATE TABLE IF NOT EXISTS player_inventory (
    world_id TEXT NOT NULL,
    player_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    quantity INTEGER NOT NULL
        CHECK (typeof(quantity) = 'integer' AND quantity >= 0),

    PRIMARY KEY (world_id, player_id, item_id),

    FOREIGN KEY (world_id, player_id)
        REFERENCES players(world_id, id)
);

CREATE TABLE IF NOT EXISTS player_item_claims (
    world_id TEXT NOT NULL,
    player_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    quantity INTEGER NOT NULL
        CHECK (typeof(quantity) = 'integer' AND quantity > 0),
    claimed_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (world_id, player_id, source_id, item_id),

    FOREIGN KEY (world_id, player_id)
        REFERENCES players(world_id, id)
);

CREATE TABLE IF NOT EXISTS carts (
	world_id TEXT NOT NULL,
	id TEXT NOT NULL,
	scene_id TEXT NOT NULL,
	position_x REAL NOT NULL,
	position_y REAL NOT NULL,
	floor INTEGER NOT NULL CHECK (floor BETWEEN 0 AND 3),

	repair_status TEXT NOT NULL
		CHECK (repair_status IN ('damaged', 'repairing', 'ready')),

	capacity_kg INTEGER NOT NULL
		CHECK (typeof(capacity_kg) = 'integer' AND capacity_kg > 0),

	revision INTEGER NOT NULL DEFAULT 1
		CHECK (revision >= 1),

	PRIMARY KEY (world_id, id),

	FOREIGN KEY (world_id, scene_id)
		REFERENCES scenes(world_id, id)
);

CREATE TABLE IF NOT EXISTS cargo_stocks (
	world_id TEXT NOT NULL,
	location_id TEXT NOT NULL,
	item_id TEXT NOT NULL,

	quantity_kg INTEGER NOT NULL
		CHECK (typeof(quantity_kg) = 'integer' AND quantity_kg >= 0),

	revision INTEGER NOT NULL DEFAULT 1
		CHECK (revision >= 1),

	PRIMARY KEY (world_id, location_id, item_id),

	FOREIGN KEY (world_id)
		REFERENCES worlds(id)
);

CREATE TABLE IF NOT EXISTS repair_jobs (
	world_id TEXT NOT NULL,
	id TEXT NOT NULL,
	cart_id TEXT NOT NULL,
	worker_npc_id TEXT NOT NULL,

	status TEXT NOT NULL
		CHECK (status IN ('running', 'completed', 'cancelled')),

	started_at TEXT NOT NULL,
	finish_at TEXT NOT NULL,

	revision INTEGER NOT NULL DEFAULT 1
		CHECK (revision >= 1),

	PRIMARY KEY (world_id, id),

	FOREIGN KEY (world_id, cart_id)
		REFERENCES carts(world_id, id),

	FOREIGN KEY (world_id, worker_npc_id)
		REFERENCES npcs(world_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_repair_jobs_running_cart
	ON repair_jobs(world_id, cart_id)
	WHERE status = 'running';

CREATE UNIQUE INDEX IF NOT EXISTS idx_repair_jobs_running_worker
	ON repair_jobs(world_id, worker_npc_id)
	WHERE status = 'running';

CREATE TABLE IF NOT EXISTS npc_inventory (
	world_id TEXT NOT NULL,
	npc_id TEXT NOT NULL,
	item_id TEXT NOT NULL,

	quantity INTEGER NOT NULL
		CHECK (typeof(quantity) = 'integer' AND quantity >= 0),

	revision INTEGER NOT NULL DEFAULT 1
		CHECK (revision >= 1),

	PRIMARY KEY (world_id, npc_id, item_id),

	FOREIGN KEY (world_id, npc_id)
		REFERENCES npcs(world_id, id)
);

CREATE TABLE IF NOT EXISTS repair_proposals (
	world_id TEXT NOT NULL,
	id TEXT NOT NULL,
	cart_id TEXT NOT NULL,

	payer_npc_id TEXT NOT NULL,
	worker_npc_id TEXT NOT NULL,

	price_coins INTEGER NOT NULL
		CHECK (typeof(price_coins) = 'integer' AND price_coins >= 0),

	material_count INTEGER NOT NULL
		CHECK (typeof(material_count) = 'integer' AND material_count > 0),

	duration_minutes INTEGER NOT NULL
		CHECK (
			typeof(duration_minutes) = 'integer'
			AND duration_minutes BETWEEN 1 AND 1440
		),

	payer_confirmed INTEGER NOT NULL DEFAULT 0
		CHECK (payer_confirmed IN (0, 1)),

	worker_confirmed INTEGER NOT NULL DEFAULT 0
		CHECK (worker_confirmed IN (0, 1)),

	status TEXT NOT NULL DEFAULT 'proposed'
		CHECK (
			status IN ('proposed', 'confirmed', 'executed', 'cancelled')
		),

	repair_job_id TEXT,

	revision INTEGER NOT NULL DEFAULT 1
		CHECK (revision >= 1),

	PRIMARY KEY (world_id, id),

	CHECK (payer_npc_id <> worker_npc_id),

	CHECK (
		status NOT IN ('confirmed', 'executed')
		OR (payer_confirmed = 1 AND worker_confirmed = 1)
	),

	CHECK (
		(status = 'executed' AND repair_job_id IS NOT NULL)
		OR (status <> 'executed' AND repair_job_id IS NULL)
	),

	FOREIGN KEY (world_id, cart_id)
		REFERENCES carts(world_id, id),

	FOREIGN KEY (world_id, payer_npc_id)
		REFERENCES npcs(world_id, id),

	FOREIGN KEY (world_id, worker_npc_id)
		REFERENCES npcs(world_id, id),

	FOREIGN KEY (world_id, repair_job_id)
		REFERENCES repair_jobs(world_id, id)
);


CREATE TABLE IF NOT EXISTS transport_jobs (
	world_id TEXT NOT NULL,
	id TEXT NOT NULL,
	cart_id TEXT NOT NULL,

	from_location_id TEXT NOT NULL,
	to_location_id TEXT NOT NULL,

	destination_scene_id TEXT NOT NULL,
	destination_x REAL NOT NULL,
	destination_y REAL NOT NULL,
	destination_floor INTEGER NOT NULL
		CHECK (destination_floor BETWEEN 0 AND 3),

	cargo_kg INTEGER NOT NULL
		CHECK (typeof(cargo_kg) = 'integer' AND cargo_kg > 0),

	status TEXT NOT NULL
		CHECK (status IN ('in_transit', 'arrived')),

	started_at TEXT NOT NULL,
	arrive_at TEXT NOT NULL,

	revision INTEGER NOT NULL DEFAULT 1
		CHECK (revision >= 1),

	PRIMARY KEY (world_id, id),

	FOREIGN KEY (world_id, cart_id)
		REFERENCES carts(world_id, id),

	FOREIGN KEY (world_id, destination_scene_id)
		REFERENCES scenes(world_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_transport_jobs_active_cart
	ON transport_jobs(world_id, cart_id)
	WHERE status = 'in_transit';


CREATE TABLE IF NOT EXISTS repair_proposal_deliveries (
    world_id TEXT NOT NULL,
    proposal_id TEXT NOT NULL,
    player_id TEXT NOT NULL,
    recipient_npc_id TEXT NOT NULL,
    source_turn_id TEXT NOT NULL,

    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    received_at TEXT,

    PRIMARY KEY (
        world_id, proposal_id, player_id, recipient_npc_id
    ),

    FOREIGN KEY (world_id, proposal_id)
        REFERENCES repair_proposals(world_id, id),

    FOREIGN KEY (world_id, player_id)
        REFERENCES players(world_id, id),

    FOREIGN KEY (world_id, recipient_npc_id)
        REFERENCES npcs(world_id, id)
);


	CREATE INDEX IF NOT EXISTS idx_repair_coordination_pending
		ON repair_coordination_events(status, created_at);

`

func OpenDatabase(ctx context.Context, path string) (*sql.DB, error) {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	dsn := filepath.ToSlash(path) +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("begin schema transaction: %w", err)
	}

	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("create game tables: %w", err)
	}

	if err := tx.Commit(); err != nil {
		db.Close()
		return nil, fmt.Errorf("commit game tables: %w", err)
	}

	return db, nil
}
