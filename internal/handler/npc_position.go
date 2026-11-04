package handler

import (
	"cybertown/internal/game/actor"
	"database/sql"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func NPCPositionsHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		positions, err := actor.GetNPCPositions(
			c.Request.Context(),
			db,
			"town-1",
		)
		if err != nil {
			log.Printf("load NPC positions: %v", err)

			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "角色位置读取失败。",
			})
			return
		}

		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{
			"npcs": positions,
		})
	}
}
