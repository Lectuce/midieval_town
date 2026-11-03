package handler

import (
	"context"
	"cybertown/internal/game"
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type PlayerGame interface {
	GetPlayer(
		context.Context,
		string,
		string,
	) (game.Player, error)

	SavePlayerPosition(
		context.Context,
		string,
		string,
		game.Position,
		int,
		int64,
	) (game.Player, error)
}

func PlayerHandler(g PlayerGame) gin.HandlerFunc {
	return func(c *gin.Context) {
		player, err := g.GetPlayer(
			c.Request.Context(),
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

			log.Printf("get player: %v", err)

			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to load player",
			})
			return
		}

		c.JSON(http.StatusOK, player)
	}
}

func PlayerPosition(g PlayerGame) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Position struct {
				X *float64 `json:"x" binding:"required"`
				Y *float64 `json:"y" binding:"required"`
			} `json:"position"`

			Floor *int `json:"floor" binding:"required,min=0,max=3"`

			ExpectedRevision int64 `json:"expected_revision" binding:"required,min=1"`
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)

		err := c.ShouldBindJSON(&body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid player position request",
			})
			return
		}

		player, err := g.SavePlayerPosition(
			c.Request.Context(),
			"town-1",
			"player-1",
			game.Position{
				X: *body.Position.X,
				Y: *body.Position.Y,
			},
			*body.Floor,
			body.ExpectedRevision,
		)

		if err != nil {
			switch {
			case errors.Is(err, game.ErrPlayerInactive):
				c.JSON(http.StatusForbidden, gin.H{
					"error": "player is not active",
				})

			case errors.Is(err, game.ErrPlayerRevisionConflict):
				c.JSON(http.StatusConflict, gin.H{
					"error": "player revision conflict; reload player",
				})

			case errors.Is(err, sql.ErrNoRows):
				c.JSON(http.StatusNotFound, gin.H{
					"error": "player not found",
				})

			default:
				log.Printf("save player position: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "failed to save player position",
				})
			}
			return
		}

		c.JSON(http.StatusOK, player)

	}
}
