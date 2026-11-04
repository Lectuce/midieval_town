package main

import (
	"context"
	agentconfig "cybertown/internal/agent/config"
	"cybertown/internal/agent/engine"
	"cybertown/internal/director"
	"cybertown/internal/game"
	"cybertown/internal/game/actor"
	"cybertown/internal/game/inventory"
	gamenegotiation "cybertown/internal/game/negotiation"
	"cybertown/internal/game/repair"
	"cybertown/internal/game/storage"
	"cybertown/internal/game/transport"
	worldstate "cybertown/internal/game/world"
	"cybertown/internal/negotiation"
	"cybertown/internal/npc"
	"cybertown/internal/rouster"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gin-gonic/gin"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := worldstate.LoadTownConfig("config/town/town.json")
	if err != nil {
		log.Fatal(err)
	}

	db, err := storage.OpenDatabase(ctx, "data/game.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	gameService := game.NewService(db)

	err = worldstate.InitWorld(ctx, db, config)
	if err != nil {
		log.Fatal(err)
	}

	err = transport.InitTransport(ctx, db, config.World.ID)
	if err != nil {
		log.Fatal(err)
	}

	err = inventory.InitNPCInventory(ctx, db, config.World.ID)
	if err != nil {
		log.Fatal(err)
	}

	repairJob, err := repair.GetActiveRepairJob(
		ctx,
		db,
		config.World.ID,
		transport.AbandonedCartID,
	)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		fmt.Println("货车当前没有正在进行的维修")

	case err != nil:
		log.Fatal(err)

	default:
		fmt.Printf(
			"维修任务：%s，维修者：%s，预计完成：%s\n",
			repairJob.ID,
			repairJob.WorkerNPCID,
			repairJob.FinishAt.Format("2006-01-02 15:04:05"),
		)
	}

	cart, err := transport.GetCart(
		ctx,
		db,
		config.World.ID,
		transport.AbandonedCartID,
	)
	if err != nil {
		log.Fatal(err)
	}

	stock, err := transport.GetCargoStock(
		ctx,
		db,
		config.World.ID,
		transport.CargoLocationCartSite,
		transport.IronOreItemID,
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf(
		"货车：%s，状态：%s，载重：%d 千克，现场铁矿：%d 千克\n",
		cart.ID,
		cart.RepairStatus,
		cart.CapacityKg,
		stock.QuantityKg,
	)

	world, err := worldstate.GetWorld(ctx, db, config.World.ID)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf(
		"推进前：%s，版本：%d\n",
		world.GameTime.Format("2006-01-02 15:04:05"),
		world.Revision,
	)

	fmt.Println("世界初始化完成，已有存档保持不变")

	fmt.Printf("配置加载成功：世界=%s，玩家=%d，NPC=%d\n",
		config.World.ID,
		len(config.Players),
		len(config.NPCs))

	fmt.Println("数据库表结构已创建：data/game.db")

	player, err := actor.GetPlayer(ctx, db, "town-1", "player-1")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf(
		"玩家：%s，场景：%s，位置：(%.2f, %.2f)，楼层：%d，版本：%d\n",
		player.Name,
		player.SceneID,
		player.Position.X,
		player.Position.Y,
		player.Floor,
		player.Revision,
	)

	if negotiationID := os.Getenv("START_MEETING_ID"); negotiationID != "" {
		meetingRoute, err := gamenegotiation.LoadNegotiationMeetingRoute(
			"config/town/negotiation_meeting.json",
		)
		if err != nil {
			log.Fatalf("load meeting route: %v", err)
		}
	}

	npcEngine, err := engine.New(
		&engine.LLM{
			Client: &agentconfig.Client,
		},
		engine.Config{
			Model:       agentconfig.PRIMARY_MODEL,
			MaxTokens:   512,
			MaxRounds:   3,
			TurnTimeout: 45 * time.Second,
		},
		engine.ToolDefinitions,
		map[string]map[string]engine.ToolHandler{
			"npc": npc.BuildToolHandlers(db),
		},
	)
	if err != nil {
		log.Fatalf("create NPC engine: %v", err)
	}
	dialogueService, err := npc.NewDialogueService(
		ctx,
		db,
		&npc.Agent{
			Engine: npcEngine,
		},
	)
	if err != nil {
		log.Fatalf("create dialogue service: %v", err)
	}

	directorEngine, err := engine.New(
		&engine.LLM{
			Client: &agentconfig.Client,
		},
		engine.Config{
			Model:       agentconfig.PRIMARY_MODEL,
			MaxTokens:   1024,
			MaxRounds:   1,
			TurnTimeout: 30 * time.Second,
		},
		map[string][]anthropic.ToolUnionParam{
			"director": {},
		},
		map[string]map[string]engine.ToolHandler{
			"director": {},
		},
	)
	if err != nil {
		log.Fatalf("create director engine: %v", err)
	}

	invitationEngine, err := engine.New(
		&engine.LLM{
			Client: &agentconfig.Client,
		},
		engine.Config{
			Model:       agentconfig.PRIMARY_MODEL,
			MaxTokens:   512,
			MaxRounds:   1,
			TurnTimeout: 30 * time.Second,
		},
		map[string][]anthropic.ToolUnionParam{
			"negotiation_invitation": {},
		},
		map[string]map[string]engine.ToolHandler{
			"negotiation_invitation": {},
		},
	)
	if err != nil {
		log.Fatalf("create invitation engine: %v", err)
	}

	workerCtx, stopWorker := context.WithCancel(context.Background())

	recoverCtx, cancelRecovery := context.WithTimeout(
		workerCtx,
		5*time.Second,
	)
	err = repair.RecoverRepairCoordination(recoverCtx, db)
	if err == nil {
		err = gamenegotiation.RecoverNegotiationInvitations(recoverCtx, db)
	}
	cancelRecovery()
	if err != nil {
		stopWorker()
		log.Fatalf("recover background events: %v", err)
	}

	directorService := &director.Service{
		DB:     db,
		Engine: directorEngine,
	}

	workerDone := make(chan struct{})

	go func() {
		defer close(workerDone)
		directorService.Run(workerCtx)
	}()

	negotiationService := &negotiation.Service{
		DB: db,
		Agent: &npc.Agent{
			Engine: invitationEngine,
		},
		MeetingRoute: meetingRoute,
	}

	negotiationDone := make(chan struct{})

	go func() {
		defer close(negotiationDone)
		negotiationService.Run(workerCtx)
	}()

	movementDone := make(chan struct{})

	go func() {
		defer close(movementDone)

		negotiationService.RunMovement(
			workerCtx,
			config.World.ID,
		)
	}()

	defer func() {
		stopWorker()
		<-workerDone
		<-negotiationDone
		<-movementDone
	}()

	r := gin.Default()
	rouster.SetUpRousters(r, db, gameService, dialogueService)

	server := &http.Server{
		Addr:              "127.0.0.1:8080",
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Println("game server listening on http://127.0.0.1:8080")

	err = server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("HTTP server stopped: %v", err)
	}

}
