package rouster

import (
	"cybertown/internal/handler"
	"cybertown/internal/npc"
	"database/sql"

	"github.com/gin-gonic/gin"
)

func SetUpRousters(r *gin.Engine, db *sql.DB, dialogueService *npc.DialogueService) {

	r.GET("/api/player", handler.PlayerHandler(db))

	chatHandler := handler.NPCChatHandler(dialogueService)

	r.POST("/api/npcs/:npcID/chat", chatHandler)
	r.POST("/api/npcs/:npcID/chat/stream", chatHandler)

	r.GET("/api/quests/:questID", handler.QuestHandler(db))
	r.POST("/api/quests/:questID/accept", handler.AcceptQuestHandler(db))

	r.PUT("/api/player/position", handler.PlayerPosition(db))
	r.POST("/api/investigation-points/abandoned_cart/inspect", handler.AbandonCart(db))
	r.GET("/api/player/inventory", handler.InventoryHandler(db))

	r.POST("/api/investigation-points/abandoned_cart/claim", handler.ClaimInventory(db))
	r.POST("/api/quests/:questID/complete", handler.CompleteQuestHandler(db))
	r.POST("/api/npcs/:npcID/repair-proposals/:proposalID/deliver", handler.DeliverRepairProposalHandler(db))
	r.GET("/api/player/repair-proposals", handler.PlayerRepairProposalsHandler(db))

}
