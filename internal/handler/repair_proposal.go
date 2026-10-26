package handler

import (
	"cybertown/internal/game"
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func DeliverRepairProposalHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		proposal, err := game.DeliverRepairProposal(
			c.Request.Context(),
			db,
			"town-1",
			"player-1",
			c.Param("proposalID"),
			c.Param("npcID"),
		)
		if err != nil {
			if errors.Is(
				err,
				game.ErrRepairProposalDeliveryDenied,
			) {
				c.JSON(http.StatusConflict, gin.H{
					"error": "这份报价无法转交给该角色。",
				})
				return
			}

			log.Printf("deliver repair proposal: %v", err)

			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "报价转交失败。",
			})
			return
		}

		log.Printf(
			"repair proposal delivered proposal=%s player=%s npc=%s",
			proposal.ID,
			"player-1",
			c.Param("npcID"),
		)

		c.JSON(http.StatusOK, gin.H{
			"message":  "报价已转交，等待对方答复。",
			"proposal": proposal,
		})
	}
}
