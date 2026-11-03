package game

import (
	"context"
	"database/sql"
	"fmt"
)

type KnownFact struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Source  string `json:"source"`
}

func GetKnownFacts(
	ctx context.Context,
	db *sql.DB,
	worldID string,
	npcID string,
) ([]KnownFact, error) {
	if worldID == "" || npcID == "" {
		return nil, fmt.Errorf("world id and NPC id are required")
	}

	// 确认这个角色确实存在于当前世界。
	if _, err := GetNPC(ctx, db, worldID, npcID); err != nil {
		return nil, fmt.Errorf("load NPC knowledge owner: %w", err)
	}

	// 只返回当前角色知道的内容。
	switch npcID {
	case "merchant":
		return []KnownFact{
			{
				ID: "merchant_iron_order",
				Content: "马丁是这批失踪铁矿货车的收货人。" +
					"这条信息不包含货车的具体位置或失踪原因。",
				Source: "本人经营的生意。",
			},
		}, nil

	case "blacksmith":
		return []KnownFact{
			{
				ID: "blacksmith_iron_need",
				Content: "罗兰的铁匠铺需要这批铁矿维持生产。" +
					"他没有亲眼见到这辆货车的行踪。",
				Source: "本人的生产需求；没有货车目击经历。",
			},
		}, nil

	case "guard", "guard_2", "guard_3":
		return []KnownFact{
			{
				ID: "guards_cart_direction",
				Content: "你值守时亲眼看见一辆货车往城门西北方向驶去。" +
					"你没有检查车上的货物，" +
					"不能确认那就是马丁等待的铁矿货车。",
				Source: "本人值守时目击。",
			},
		}, nil

	default:
		// 空列表表示尚未配置相关信息，不是请求失败。
		return []KnownFact{}, nil
	}
}
