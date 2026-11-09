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

var _blend_start := Vector2.ZERO
var _blend_elapsed := 0.0
var _blend_duration := 0.25

var _position_pending := false
var _snap_pending := false
var _last_position_time_ms: int = 0


func apply_server_position(
	snapshot: Dictionary,
	world_root: Node2D
) -> void:
	if npc_id not in ["merchant", "blacksmith"]:
		return

	if not is_instance_valid(world_root):
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

	var target := world_root.to_global(town_position)
	var now_ms := Time.get_ticks_msec()
	var first_position := _server_revision < 0

	_server_revision = revision

	# 第一次进入游戏：直接显示存档位置，不播放过去的移动。
	if first_position:
		_server_target = target
		_last_position_time_ms = now_ms
		_snap_pending = true
		_position_pending = false
		return

	# 活动变化也可能增加版本；坐标没变，不要重新开始补间。
	if target.is_equal_approx(_server_target):
		return

	var sample_interval := float(
		now_ms - _last_position_time_ms
	) / 1000.0
	_last_position_time_ms = now_ms

	_server_target = target

	# 长时间断线后的明显位置差异直接校正，
	# 避免角色从旧位置缓慢穿过半张地图。
	if _snap_pending or global_position.distance_to(target) > 96.0:
		_snap_pending = true
		_position_pending = false
		return

	_blend_start = global_position
	_blend_elapsed = 0.0
	_blend_duration = clampf(sample_interval, 0.10, 0.50)
	_position_pending = true


func _physics_process(delta: float) -> void:
	if _snap_pending:
		global_position = _server_target
		_snap_pending = false
		return

	if not _position_pending:
		return

	_blend_elapsed += delta

	var weight := clampf(
		_blend_elapsed / _blend_duration,
		0.0,
		1.0
	)

	global_position = _blend_start.lerp(_server_target, weight)

	if weight >= 1.0:
		global_position = _server_target
		_position_pending = false
