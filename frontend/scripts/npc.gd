extends AnimatableBody2D

## Must match the NPC id in the server's town configuration.
@export var npc_id: String = ""
@export var display_name: String = "居民"
@export var dialogue_lines: Array[String] = ["你好，欢迎来到小镇。"]
@export_range(8.0, 48.0) var talk_distance: float = 24.0
@export var reply_rules: Dictionary = {
	"你好|您好|hello": "你好！想打听什么事情？",
	"谢谢|感谢": "不客气，祝你在镇上过得愉快。",
	"再见|拜拜": "回头见，一路顺风。",
	"城堡|上楼|下楼|楼梯": "从城堡正门进入一楼，左侧楼梯通往二楼，中央梯子通往三楼。靠近楼梯按 E 就能上下楼。",
}
@export_multiline var fallback_reply: String = "这件事我还不太清楚。你可以问我“你是谁”或“城堡怎么走”。"

func _ready() -> void:
	add_to_group("talkable_npcs")




func reply_to(message: String) -> String:
	var query := message.to_lower()
	if "你是谁" in query or "名字" in query:
		return "我是%s。" % display_name
	if "这里" in query or "做什么" in query or "介绍" in query:
		return dialogue_lines[0] if not dialogue_lines.is_empty() else "欢迎来到小镇。"
	for keywords in reply_rules:
		for keyword in str(keywords).split("|", false):
			var term := keyword.strip_edges().to_lower()
			if not term.is_empty() and term in query:
				return str(reply_rules[keywords])
	return fallback_reply


var _server_revision: int = -1
var _server_target := Vector2.ZERO
var _position_pending := false


func apply_server_position(
	snapshot: Dictionary,
	world_root: Node2D
) -> void:
	# 这一版只同步地面上的会面参与者。
	if npc_id not in ["merchant", "blacksmith"]:
		return

	if str(snapshot.get("id", "")) != npc_id:
		return

	if str(snapshot.get("scene_id", "")) != "town":
		return

	if int(snapshot.get("floor", -1)) != 0:
		return

	var revision := int(snapshot.get("revision", -1))
	if revision < 1 or revision <= _server_revision:
		return

	var position_data = snapshot.get("position")
	if not position_data is Dictionary:
		return

	var x_value = position_data.get("x")
	var y_value = position_data.get("y")

	if not (x_value is float or x_value is int):
		return
	if not (y_value is float or y_value is int):
		return

	var town_position := Vector2(
		float(x_value),
		float(y_value)
	)

	if not is_finite(town_position.x) or not is_finite(town_position.y):
		return

	_server_revision = revision

	# 后端保存的是相对 Game 的坐标。
	# 转成全局坐标后再应用，避免 Game 自身偏移造成错位。
	_server_target = world_root.to_global(town_position)
	_position_pending = true


func _physics_process(_delta: float) -> void:
	if not _position_pending:
		return

	global_position = _server_target
	_position_pending = false
