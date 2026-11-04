package handler

import (
	constError "cybertown/internal/game/const_error"
	"cybertown/internal/game/inventory"
	"cybertown/internal/game/quest"
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func InventoryHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := inventory.GetInventory(
			c.Request.Context(),
			db,
			"town-1",
			"player-1",
		)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				c.JSON(http.StatusNotFound, gin.H{
					"error": "player not found",
				})
				return
			}

			log.Printf("get inventory: %v", err)

			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to load inventory",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"items": items,
		})
	}
}

func ClaimInventory(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		result, err := quest.ClaimShipmentReceipt(c.Request.Context(), db, "town-1", "player-1")
		if err != nil {
			switch {
			case errors.Is(err, constError.ErrPlayerInactive):
				c.JSON(http.StatusForbidden, gin.H{
					"error": "玩家当前无法领取物品。",
				})

			case errors.Is(err, constError.ErrQuestNotActive):
				c.JSON(http.StatusConflict, gin.H{
					"error": "需要先接受货车调查任务，且任务尚未完成。",
				})

			case errors.Is(err, constError.ErrEvidenceRequired):
				c.JSON(http.StatusConflict, gin.H{
					"error": "请先调查货车现场。",
				})

			case errors.Is(err, constError.ErrOutsideInvestigationRange):
				c.JSON(http.StatusForbidden, gin.H{
					"error": "请靠近货车现场后再领取。",
				})

			case errors.Is(err, sql.ErrNoRows):
				c.JSON(http.StatusNotFound, gin.H{
					"error": "玩家不存在。",
				})

			default:
				log.Printf("claim shipment receipt: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "领取失败，请稍后重试。",
				})
			}
			return

		}
		c.JSON(http.StatusOK, result)
	}

}
