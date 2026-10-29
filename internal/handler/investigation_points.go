package handler

import (
	"cybertown/internal/game"
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func AbandonCart(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, err := game.InspectAbandonedCart(
			c.Request.Context(),
			db,
			"town-1",
			"player-1",
		)
		if err != nil {
			switch {
			case errors.Is(err, game.ErrPlayerInactive):
				c.JSON(http.StatusForbidden, gin.H{
					"error": "玩家当前无法调查。",
				})

			case errors.Is(err, game.ErrQuestNotActive):
				c.JSON(http.StatusConflict, gin.H{
					"error": "需要先接受货车调查任务，且任务尚未完成。",
				})

			case errors.Is(err, game.ErrOutsideInvestigationRange):
				c.JSON(http.StatusForbidden, gin.H{
					"error": "请靠近货车现场后再调查。",
				})

			case errors.Is(err, sql.ErrNoRows):
				c.JSON(http.StatusNotFound, gin.H{
					"error": "玩家不存在。",
				})

			default:
				log.Printf("inspect abandoned cart: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "调查保存失败，请稍后重试。",
				})
			}
			return
		}

		c.JSON(http.StatusOK, result)
	}

}
