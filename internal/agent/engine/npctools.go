package engine

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
)

var NPCToolHandlers = map[string]ToolHandler{
	"get_agent_id": func(ctx context.Context, scope Scope, input map[string]any) (string, error) {
		return scope.AgentID, nil
	},
}

var NPCTools = []anthropic.ToolParam{

	{

		Name:        "get_agent_id",
		Description: anthropic.String("Get the current NPC's ID."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Type:       "object",
			Properties: map[string]interface{}{},
			Required:   []string{},
		},
	},
	{
		Name: "get_my_quest_status",
		Description: anthropic.String(
			"查询当前角色发布的委托记录和约定。" +
				"返回是否接取、是否完成结算、所需凭证及奖励规则。" +
				"不提供玩家背包数量、未告知的调查发现或提交条件是否齐全。" +
				"任务进行中不代表玩家缺少凭证。" +
				"仅委托发布者可使用，只查询，不执行交付或结算。",
		),
		InputSchema: anthropic.ToolInputSchemaParam{
			Type:       "object",
			Properties: map[string]interface{}{},
			Required:   []string{},
		},
	},
	{
		Name: "get_known_facts",
		Description: anthropic.String(
			"查询当前角色知道的货车调查信息及其来源。" +
				"只返回当前角色的信息，不提供其他角色的秘密。" +
				"询问货车行踪、相关经历或调查线索时使用。" +
				"信息中的不确定性必须保留，不能把猜测说成目击事实。" +
				"此工具不记录玩家发现证据，也不改变任务状态。",
		),
		InputSchema: anthropic.ToolInputSchemaParam{
			Type:       "object",
			Properties: map[string]interface{}{},
			Required:   []string{},
		},
	},

	{
		Name: "get_my_repair_resources",
		Description: anthropic.String(
			"查询当前角色自己的资金和修车材料。" +
				"仅商人马丁、铁匠罗兰可使用。" +
				"不能查询其他角色的库存，也不会转移资源。",
		),
		InputSchema: anthropic.ToolInputSchemaParam{
			Type:       "object",
			Properties: map[string]any{},
			Required:   []string{},
		},
	},
	{
		Name: "propose_cart_repair",
		Description: anthropic.String(
			"铁匠罗兰提出一份由马丁付款、罗兰提供材料的货车维修报价，" +
				"并确认本人愿意按该条件维修。" +
				"调用前先查询自己的资源。" +
				"只确认罗兰本人，不代表马丁同意、收到提议或已经付款。" +
				"不会开始维修。每轮对话最多创建一份报价。",
		),
		InputSchema: anthropic.ToolInputSchemaParam{
			Type: "object",
			Properties: map[string]any{
				"price_coins": map[string]any{
					"type":        "integer",
					"minimum":     0,
					"description": "向马丁提出的维修费用。",
				},
				"material_count": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"description": "罗兰愿意提供并在开工时消耗的材料份数。",
				},
				"duration_minutes": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     1440,
					"description": "从实际开工起计算的游戏分钟数。",
				},
			},
			Required: []string{
				"price_coins",
				"material_count",
				"duration_minutes",
			},
		},
	},

	{
		Name: "get_received_repair_proposals",
		Description: anthropic.String(
			"仅马丁可用。查询玩家已经明确转交给自己的维修报价，" +
				"同时返回本人的当前资金。" +
				"不返回尚未送达的报价，也不执行接受、付款或维修。",
		),
		InputSchema: anthropic.ToolInputSchemaParam{
			Type:       "object",
			Properties: map[string]any{},
			Required:   []string{},
		},
	},
	{
		Name: "accept_repair_proposal",
		Description: anthropic.String(
			"仅马丁可用。接受一份已收到的维修报价，确认自己作为付款方的意愿。" +
				"必须先查询实际报价，使用返回的报价ID和版本。" +
				"可以拒绝，不必接受每份报价。" +
				"成功只表示接受条款，不表示已经付款或开始维修。",
		),
		InputSchema: anthropic.ToolInputSchemaParam{
			Type: "object",
			Properties: map[string]any{
				"proposal_id": map[string]any{
					"type": "string",
				},
				"expected_revision": map[string]any{
					"type":    "integer",
					"minimum": 1,
				},
			},
			Required: []string{
				"proposal_id",
				"expected_revision",
			},
		},
	},

	{
		Name: "respond_to_repair_proposal",
		Description: anthropic.String(
			"仅马丁可用。对已经收到的维修报价记录拒绝或修改要求。" +
				"先查询实际报价，使用对应ID和版本。" +
				"reject表示拒绝，不请求继续协商；" +
				"request_revision表示提出具体修改要求，并允许系统组织传达。" +
				"不会改价、替罗兰同意、付款或启动维修。" +
				"普通讨论不必调用，不得为了触发协商故意拒绝。",
		),
		InputSchema: anthropic.ToolInputSchemaParam{
			Type: "object",
			Properties: map[string]any{
				"proposal_id": map[string]any{
					"type": "string",
				},
				"expected_revision": map[string]any{
					"type":    "integer",
					"minimum": 1,
				},
				"decision": map[string]any{
					"type": "string",
					"enum": []string{"reject", "request_revision"},
				},
				"reason": map[string]any{
					"type":      "string",
					"minLength": 1,
					"maxLength": 256,
				},
			},
			Required: []string{
				"proposal_id",
				"expected_revision",
				"decision",
				"reason",
			},
		},
	},
}

var NPCToolsDefinitions = map[string][]anthropic.ToolUnionParam{
	"npc": BuildTools(NPCTools),
}

func BuildTools(tools []anthropic.ToolParam) []anthropic.ToolUnionParam {

	toolUnions := make([]anthropic.ToolUnionParam, 0, len(tools))

	for i, _ := range tools {
		toolParam := tools[i]
		toolUnions = append(toolUnions,
			anthropic.ToolUnionParam{
				OfTool: &toolParam,
			},
		)
	}

	return toolUnions
}
