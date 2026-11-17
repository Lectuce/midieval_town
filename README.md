
# Cyber Town

> Agent-Driven Narrative RPG

一款正在开发中的 AI 单人叙事 RPG。玩家来到一座依赖铁矿贸易的中世纪小镇，通过自由语言与商人、铁匠、守卫和酒馆老板交涉，调查一辆离奇失踪的铁矿货车。

项目的重点不是让大模型“扮演整个世界”，而是让 Agent 成为游戏系统中的受限参与者：NPC Agent 理解玩家表达、维持角色立场并申请合法行动；确定性的 GameService 保存事实、校验规则并执行状态变更；导演 Agent 只在关键事件后规划可验证的后续机会。

## 项目亮点

- **自由语言驱动玩法**：玩家无需从固定选项中选择，可以直接询问、解释、交涉或提出条件。
- **Agent 与规则分层**：模型负责语言理解和开放式决策，任务、证据、位置与奖励始终由游戏服务裁决。
- **角色级上下文隔离**：会话按 `WorldID + PlayerID + NPCID` 隔离，NPC 只获得自己的身份与对话历史。
- **可控的工具调用**：共享 Engine 通过服务端选定的 ToolSet 限制能力，并将不可授权的调用挡在规则边界之外。
- **流式游戏对话**：Gin 提供 SSE 接口，Godot 客户端可以逐段展示 NPC 回复。
- **可恢复的权威状态**：SQLite 持久化世界、玩家、NPC、任务、证据和对话；初始化不会覆盖已有存档。
- **并发与幂等保护**：玩家位置使用版本号做乐观并发控制；对话使用 `request_id` 避免重复模型调用和重复落库。

## 核心玩法

第一版围绕“失踪的铁矿货车”展开：

1. 玩家向商人马丁了解失踪货物并接受调查任务。
2. 玩家在小镇中移动，与不同 NPC 交谈，拼接各自掌握的信息。
3. 玩家前往货车现场进行调查，游戏服务根据任务状态、坐标和楼层校验行为。
4. 找到的证据被写入存档，并在后续对话和任务流程中成为可验证事实。
5. 领取货物凭证，通过交付按钮完成调查任务并获得一次性奖励。
6. 向罗兰取得维修报价，转交马丁，或邀请双方会面并旁听自主协商。
7. 后续重点是补齐 NPC 自身需求、合法维修方式和导演的具体澄清，让协商影响真实维修结果。

## 架构设计

```mermaid
flowchart LR
    Godot[Godot 客户端] -->|HTTP / SSE| API[Gin API]
    API --> Dialogue[NPC 对话服务]
    API --> Game[GameService]
    Dialogue --> Engine[共享 Agent Engine]
    Engine --> LLM[LLM API]
    Engine --> Tools[受限工具集]
    Tools --> Game
    Dialogue --> DB[(SQLite)]
    Game --> DB
    DB -. 协商分歧事件 .-> Director[导演 Agent · 已有基础编排]
    Director -. 合法计划 .-> Game
```

### 职责边界

| 模块 | 职责 | 是否拥有最终决定权 |
| --- | --- | --- |
| NPC Agent | 理解玩家表达、生成符合人设的回应、申请角色行动 | 否 |
| 导演 Agent | 根据公开分歧组织澄清、请求反报价或停止，不替角色同意 | 否 |
| Agent Engine | 执行模型—工具循环，控制轮数、超时和工具权限 | 否 |
| GameService | 校验位置、任务、证据、物品和状态版本 | 是 |
| SQLite | 保存可恢复的权威游戏状态与对话记录 | 是 |

这条边界解决了生成式游戏最关键的问题：NPC 可以灵活回应，但不能因为一句台词就凭空创造证据、完成任务或发放奖励。

## 当前进度

更新日期：2026-11-17。以下“已有实现”指本地源码中已有对应入口和逻辑，
不代表本次已执行完整测试或确认 Linux 正在运行相同版本。

| 能力 | 状态 |
| --- | --- |
| 小镇、玩家与 6 名 NPC 的配置化初始化 | ✅ 已完成 |
| 玩家坐标、楼层与版本号持久化 | ✅ 已完成 |
| 共享 Agent Engine 与工具调用循环 | ✅ 已完成 |
| NPC 人设提示词与独立对话历史 | ✅ 已完成 |
| 普通 HTTP 对话与 SSE 流式输出 | ✅ 已完成 |
| 请求幂等、会话串行化与基础限流 | ✅ 已完成 |
| 货车任务接受、现场范围校验与证据入库 | ✅ 已完成 |
| Godot 网页对话、调查、背包和报价面板 | 已有实现；部署后需按实际网页验收 |
| 凭证领取、任务提交、物品校验与一次性奖励 | 已有实现 |
| NPC 已知事实、委托查询与维修报价工具 | 已有实现；不等于完整出示证据或物品交换系统 |
| 维修报价取得、转交和双方确认 | 已有实现 |
| 维修耗材、付款、游戏时间结算及货物装载/运输服务 | 已有规则实现；不等于全部玩法已在客户端接通 |
| 邀请、NPC 自主回应、会面移动与位置插值 | 已有实现 |
| 会面发言查询、旁听面板与插话接口 | 已有实现；需结合实际部署验证体验 |
| 自动报价评估、反报价及有限导演编排 | 已有基础事件链与 PlanDiscussion |
| 马丁的持久化维修需求、罗兰的合法修法目录 | 已实现数据基础、缺失初始化及条款校验函数；尚未接入报价和 Agent 输入 |
| 紧凑对话面板 | 已改为对话/委托/维修协商页签、响应式底部面板和内部滚动 |
| 导演具体问题送达、基于新条件的进展判断 | 待完善 |
| 协商协议自动接通维修执行及结果展示 | 待完成完整闭环 |
| 导演游戏价值的连续场景验收 | 待完成，不能以模型调用成功替代 |
| NPC 长期记忆、模型分级与完整降级策略 | 待完善；已有超时和调用预算不代表全部完成 |

> Godot 客户端源码位于 `frontend/`。Linux 的进程、存档和网页导出包属于部署状态，须单独核对。
> 当前 `agreed` 只表示协议成立，不表示已扣款、开工或修好。实际货车维修完成状态为 `ready`。

## 技术栈

- **客户端**：Godot
- **服务端**：Go、Gin
- **Agent**：Anthropic Go SDK、自研模型—工具执行循环
- **存储**：SQLite（权威状态与会话）
- **长期记忆预研**：Qdrant（不承担任务、物品或世界事实判断）
- **通信**：REST、Server-Sent Events

## 快速开始

### 环境要求

- Go `1.26.5`（以 `go.mod` 为准）
- 一个与 Anthropic Messages API 兼容的模型服务

### 1. 配置模型

当前程序直接读取环境变量，不会自动加载 `.env`：

```powershell
$env:API_KEY = "your-api-key"
$env:MODEL = "your-model-name"

# 使用兼容服务时再设置
$env:BASE_URL = "https://your-endpoint.example/v1"
```

### 2. 启动游戏服务

```powershell
go run .
```

服务默认监听 `http://127.0.0.1:18080`。可通过 `SERVER_ADDR` 覆盖，例如
`SERVER_ADDR=127.0.0.1:9000 go run .`。首次启动会根据 `config/town/town.json`
创建 `data/game.db`；后续启动会保留已有存档。

### 3. 验证核心接口

```powershell
# 查询玩家
Invoke-RestMethod http://127.0.0.1:18080/api/player

# 接受货车调查任务
Invoke-RestMethod `
  -Method Post `
  http://127.0.0.1:18080/api/quests/missing_iron_cart/accept

# 与商人对话
$body = @{
  request_id = [guid]::NewGuid().ToString()
  message = "那辆铁矿货车最后在哪里出现？"
} | ConvertTo-Json

Invoke-RestMethod `
  -Method Post `
  -ContentType "application/json" `
  -Body $body `
  http://127.0.0.1:18080/api/npcs/merchant/chat
```

流式对话端点为：

```text
POST /api/npcs/:npcID/chat/stream
```

它以 SSE `delta` 事件发送增量文本，并以 `done` 事件返回已经持久化的完整结果。

## API 概览

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/player` | 查询当前玩家状态 |
| `PUT` | `/api/player/position` | 保存坐标、楼层和状态版本 |
| `POST` | `/api/npcs/:npcID/chat` | 获取完整 NPC 回复 |
| `POST` | `/api/npcs/:npcID/chat/stream` | 以 SSE 获取流式 NPC 回复 |
| `GET` | `/api/quests/:questID` | 查询任务定义与玩家进度 |
| `POST` | `/api/quests/:questID/accept` | 接受任务 |
| `POST` | `/api/investigation-points/abandoned_cart/inspect` | 调查废弃货车并记录证据 |
| `POST` | `/api/investigation-points/abandoned_cart/claim` | 领取货物凭证 |
| `GET` | `/api/player/inventory` | 查询背包 |
| `POST` | `/api/quests/:questID/complete` | 交付凭证并结算奖励 |
| `GET` | `/api/player/repair-proposals` | 查询玩家取得的报价卡 |
| `POST` | `/api/npcs/:npcID/repair-proposals/:proposalID/deliver` | 转交报价 |
| `POST` | `/api/negotiations` | 发起会面邀请 |
| `GET` | `/api/negotiations/:id` | 查询会面、公开发言和当前报价 |
| `POST` | `/api/negotiations/:id/messages` | 会面中插话 |
| `POST` | `/api/negotiations/:id/cancel` | 明确取消会面 |
| `GET` | `/api/player/negotiations/latest` | 查询最近会面 |
| `GET` | `/api/npcs/positions` | 查询 NPC 权威位置 |

当前接口使用固定的开发身份 `town-1 / player-1`，尚未实现生产级账号与鉴权。

## 项目结构

```text
.
├── main.go                      # 游戏 HTTP 服务及后台任务入口
├── frontend/                    # Godot 项目和网页交互脚本
├── config/town/                 # 世界、玩家与 NPC 初始配置
├── internal/agent/engine/       # 可复用的模型—工具执行核心
├── internal/game/               # 权威状态、任务、调查与 SQLite 存储
├── internal/handler/            # HTTP 参数校验与响应映射
├── internal/npc/                # NPC 提示词、Agent 与对话服务
├── internal/director/           # 导演计划生成与旧协调事件处理
├── internal/negotiation/        # 邀请、移动与自主协商后台调度
├── internal/rouster/            # Gin 路由注册
└── docs/                        # 开发大纲与早期技术方案
```

## 测试

```powershell
go test ./...
go vet ./...
```

Agent Engine 使用假模型覆盖两条关键路径：直接文本回复，以及工具调用后继续生成最终回复。测试不需要真实 API Key。

## 路线图

1. 已建立马丁需求和两种修法的数据基础，初始化保留已有需求及旧报价；完整业务接入仍待下一项。
2. **当前下一步**：将修法和需求版本接入首次报价、反报价、确认和执行的统一校验。
3. 按角色认知范围注入需求/能力，让导演提出具体、有来源的澄清或反报价请求。
4. 自动送达并触发 NPC 回应，依据新信息调整安排；直接接受不调用导演，无进展则停止。
5. 接通协议后的合法维修执行、公开结果展示及连续场景验收。

马丁决定自己的需求和付款偏好，罗兰决定修法与接单条件；玩家无需代填协调目标。
后续以 [`docs/npc-negotiation-implementation-guide.md`](docs/npc-negotiation-implementation-guide.md)
中的 **2026-11-09 正式实施基线**为准，按第 12 节实施、第 13～14 节验收。
这些本地文档和 AGENTS.md 当前被 Git 忽略，普通推送不会自动同步它们。

详细方向见 [`docs/cybertown-development-outline.md`](docs/cybertown-development-outline.md)。早期架构讨论保留在 [`docs/medieval-town-agent-plan.md`](docs/medieval-town-agent-plan.md)，如有冲突，以当前开发大纲和实际代码为准。

## 设计原则

```text
玩家自由表达
→ NPC Agent 理解并提出合法行动
→ GameService 校验、执行并保存真实变化
→ 重要事件触发导演 Agent
→ 导演提出可执行的后续计划
→ GameService 再次校验并落地
→ 玩家在世界中看到变化
```

**模型负责可能性，规则负责真实性。**
