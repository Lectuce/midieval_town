extends Node

var world_root: Node2D
var api_url: String = ""

var _http: HTTPRequest
var _timer: Timer
var _busy := false
var _failure_reported := false


func _ready() -> void:
	_http = HTTPRequest.new()
	_http.timeout = 5.0
	_http.body_size_limit = 65536
	add_child(_http)

	_http.request_completed.connect(_on_completed)

	_timer = Timer.new()
	_timer.wait_time = 0.25
	_timer.timeout.connect(_poll)
	add_child(_timer)
	_timer.start()

	# 延后到当前场景初始化结束，确保 NPC 已加入分组。
	call_deferred("_poll")


func _poll() -> void:
	if _busy or api_url.is_empty():
		return

	if not is_instance_valid(world_root):
		return

	_busy = true

	var err := _http.request(
		api_url,
		PackedStringArray([
			"Accept: application/json",
			"Cache-Control: no-cache",
		])
	)

	if err != OK:
		_busy = false
		_report_failure("无法发起 NPC 位置请求，错误码：%s" % err)


func _on_completed(
	result: int,
	status_code: int,
	_headers: PackedStringArray,
	body: PackedByteArray
) -> void:
	_busy = false

	if result != HTTPRequest.RESULT_SUCCESS:
		_report_failure("NPC 位置请求失败，错误码：%s" % result)
		return

	if status_code != 200:
		_report_failure("NPC 位置接口返回 HTTP %s" % status_code)
		return

	var data = JSON.parse_string(body.get_string_from_utf8())
	if not data is Dictionary:
		_report_failure("NPC 位置响应不是 JSON 对象。")
		return

	var snapshots = data.get("npcs")
	if not snapshots is Array:
		_report_failure("NPC 位置响应缺少 npcs 数组。")
		return

	_failure_reported = false
	_timer.wait_time = 0.25

	var nodes_by_id: Dictionary = {}

	for node in get_tree().get_nodes_in_group("talkable_npcs"):
		if node.has_method("apply_server_position"):
			nodes_by_id[str(node.npc_id)] = node

	for snapshot in snapshots:
		if not snapshot is Dictionary:
			continue

		var id := str(snapshot.get("id", ""))
		var node = nodes_by_id.get(id)

		if is_instance_valid(node):
			node.apply_server_position(snapshot, world_root)


func _report_failure(message: String) -> void:
	# 失败时降低频率，并避免每次轮询重复刷日志。
	_timer.wait_time = 2.0

	if not _failure_reported:
		push_warning(message)
		_failure_reported = true


func _exit_tree() -> void:
	if is_instance_valid(_http):
		_http.cancel_request()