package handler

import (
	"cybertown/internal/game"
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func CreateNegotiationHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			RequestID        string `json:"request_id" binding:"required,max=128"`
			ProposalID       string `json:"proposal_id" binding:"required,max=128"`
			ExpectedRevision int64  `json:"expected_revision" binding:"required,min=1"`
		}

		c.Request.Body = http.MaxBytesReader(
			c.Writer,
			c.Request.Body,
			4096,
		)

		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "邀请参数不正确。",
			})
			return
		}

		// 延续当前单玩家接口的身份设置。
		// 不允许客户端指定其他玩家或报价参与者。
		negotiation, err := game.CreateNegotiation(
			c.Request.Context(),
			db,
			"town-1",
			"player-1",
			game.CreateNegotiationRequest{
				RequestID:        body.RequestID,
				ProposalID:       body.ProposalID,
				ExpectedRevision: body.ExpectedRevision,
			},
		)
		if err != nil {
			switch {
			case errors.Is(err, game.ErrNegotiationInvalidRequest):
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "邀请参数不正确。",
				})

			case errors.Is(err, game.ErrNegotiationRequestConflict):
				c.JSON(http.StatusConflict, gin.H{
					"error": "该请求编号已经用于另一份报价或版本。",
				})

			case errors.Is(err, game.ErrRepairProposalRevisionConflict):
				c.JSON(http.StatusConflict, gin.H{
					"error": "报价已经变化，请刷新报价后再邀请。",
				})

			case errors.Is(err, game.ErrNegotiationUnavailable):
				c.JSON(http.StatusConflict, gin.H{
					"error": "当前报价或货车状态不允许发起协商。",
				})

			case errors.Is(err, game.ErrNegotiationBusy):
				c.JSON(http.StatusConflict, gin.H{
					"error": "货车或参与者已有协商、工作或已确认的维修安排。",
				})

			default:
				log.Printf("create negotiation: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "创建协商邀请失败。",
				})
			}
			return
		}

		// 当前 Negotiation 没有 JSON 标签，
		// 在这里明确返回前端使用的字段名。
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{
			"negotiation": gin.H{
				"id":                        negotiation.ID,
				"request_id":                negotiation.RequestID,
				"current_proposal_id":       negotiation.CurrentProposalID,
				"current_proposal_revision": negotiation.CurrentProposalRevision,
				"status":                    negotiation.Status,
				"next_actor_id":             negotiation.NextActorID,
				"revision":                  negotiation.Revision,
			},
		})
	}
}
