package handler

import (
	"cybertown/internal/game/actor"
	constError "cybertown/internal/game/const_error"
	"cybertown/internal/game/quest"
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 查询任务介绍和当前玩家进度。
func QuestHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		questID := c.Param("questID")

		definition, err := quest.GetQuestDefinition(questID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "quest not found",
			})
			return
		}

		// 确认玩家存在
		_, err = actor.GetPlayer(ctx, db, "town-1", "player-1")
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				c.JSON(http.StatusNotFound, gin.H{
					"error": "player not found",
				})
				return
			}

			log.Printf("load quest player: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to load player",
			})
			return
		}

		quest, err := quest.GetPlayerQuest(ctx, db, "town-1", "player-1", questID)

		status := "not_started"
		if err == nil {
			status = quest.Status
		} else if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("get player quest: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to load quest",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"quest_id":     definition.ID,
			"title":        definition.Title,
			"description":  definition.Description,
			"giver_npc_id": definition.GiverNPCID,
			"status":       status,
		})
	}
}

// 接受任务，保存玩家进度。
func AcceptQuestHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		questID := c.Param("questID")

		// 判断存不存在任务，返回明确的 404。
		_, err := quest.GetQuestDefinition(questID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "quest not found",
			})
			return
		}

		quest, err := quest.AcceptQuest(
			c.Request.Context(),
			db,
			"town-1",
			"player-1",
			questID,
		)
		if err != nil {
			switch {
			case errors.Is(err, constError.ErrPlayerInactive):
				c.JSON(http.StatusForbidden, gin.H{
					"error": "player is not active",
				})

			case errors.Is(err, sql.ErrNoRows):
				c.JSON(http.StatusNotFound, gin.H{
					"error": "player or quest giver not found",
				})

			default:
				log.Printf("accept quest: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "failed to accept quest",
				})
			}
			return
		}

		c.JSON(http.StatusOK, quest)
	}
}

func CompleteQuestHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		questID := c.Param("questID")

		// 将不存在的任务与结算失败区分开。
		if _, err := quest.GetQuestDefinition(questID); err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "任务不存在。",
			})
			return
		}

		result, err := quest.CompleteQuest(
			c.Request.Context(),
			db,
			"town-1",
			"player-1",
			questID,
		)
		if err != nil {
			switch {
			case errors.Is(err, constError.ErrPlayerInactive):
				c.JSON(http.StatusForbidden, gin.H{
					"error": "玩家当前无法提交任务。",
				})

			case errors.Is(err, constError.ErrQuestNotActive):
				c.JSON(http.StatusConflict, gin.H{
					"error": "任务尚未接受或不处于进行中。",
				})

			case errors.Is(err, constError.ErrOutsideQuestRange):
				c.JSON(http.StatusForbidden, gin.H{
					"error": "请靠近任务发布者后再提交。",
				})

			case errors.Is(err, constError.ErrEvidenceRequired):
				c.JSON(http.StatusConflict, gin.H{
					"error": "缺少调查证据，请先调查货车现场。",
				})

			case errors.Is(err, constError.ErrQuestItemsRequired):
				c.JSON(http.StatusConflict, gin.H{
					"error": "缺少货物凭证，请先从货车现场领取。",
				})

			case errors.Is(err, constError.ErrQuestCompletionConflict):
				c.JSON(http.StatusConflict, gin.H{
					"error": "任务状态已变化，请重新查询后再提交。",
				})

			case errors.Is(err, sql.ErrNoRows):
				c.JSON(http.StatusNotFound, gin.H{
					"error": "玩家或任务发布者不存在。",
				})

			default:
				log.Printf("complete quest: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "任务结算失败，请稍后重试。",
				})
			}
			return
		}

		c.JSON(http.StatusOK, result)
	}
}
