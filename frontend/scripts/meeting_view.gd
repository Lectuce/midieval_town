extends AcceptDialog

signal snapshot_received(session: Dictionary)

var _base_url := ""
var _session_id := ""

var _status: Label
var _text: RichTextLabel
var _timer: Timer
var _request: HTTPRequest

var _generation := 0
var _busy := false


func _ready() -> void:
	title = "马丁与罗兰的会面"
	min_size = Vector2i(300, 260)
	get_ok_button().text = "关闭查看"

	var column := VBoxContainer.new()
	column.add_theme_constant_override("separation", 12)
	add_child(column)

	_status = Label.new()
	_status.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	column.add_child(_status)

	_text = RichTextLabel.new()
	_text.bbcode_enabled = false
	_text.fit_content = false
	_text.selection_enabled = true
	_text.scroll_following = true
	_text.custom_minimum_size.y = 140
	_text.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	_text.size_flags_vertical = Control.SIZE_EXPAND_FILL
	_text.add_theme_font_size_override("normal_font_size", 20)
	column.add_child(_text)

	_timer = Timer.new()
	_timer.wait_time = 1.5
	_timer.timeout.connect(_poll)
	add_child(_timer)

	visibility_changed.connect(_on_visibility_changed)


func open_view(base_url: String, session_id: String = "") -> void:
	_stop_polling()

	_base_url = base_url.trim_suffix("/")
	_session_id = session_id
	_status.text = "正在读取会面记录…"
	_text.text = ""

	popup_centered_clamped(Vector2i(760, 480), 0.9)

	_timer.start()
	_poll()


func close_view() -> void:
	hide()
	_stop_polling()


func _on_visibility_changed() -> void:
	if not visible:
		_stop_polling()


func _stop_polling() -> void:
	_generation += 1
	_busy = false

	if is_instance_valid(_timer):
		_timer.stop()

	if is_instance_valid(_request):
		_request.cancel_request()
		_request.queue_free()
		_request = null


func _poll() -> void:
	if not visible or _busy or _base_url.is_empty():
		return

	var path := "/api/player/negotiations/latest"
	if not _session_id.is_empty():
		path = "/api/negotiations/" + _session_id.uri_encode()

	_busy = true
	_generation += 1
	var generation := _generation

	_request = HTTPRequest.new()
	_request.timeout = 10.0
	_request.body_size_limit = 262144
	_request.request_completed.connect(
		_on_completed.bind(generation)
	)
	add_child(_request)

	var err := _request.request(
		_base_url + path,
		PackedStringArray([
			"Accept: application/json",
			"Cache-Control: no-cache"
		])
	)

	if err != OK:
		_busy = false
		_request.queue_free()
		_request = null
		_status.text = "会面请求没有发出，稍后自动重试。"


func _on_completed(
	result: int,
	code: int,
	_headers: PackedStringArray,
	body: PackedByteArray,
	generation: int
) -> void:
	if generation != _generation:
		return

	_busy = false

	if is_instance_valid(_request):
		_request.queue_free()
		_request = null

	if result != HTTPRequest.RESULT_SUCCESS:
		_status.text = "会面记录更新失败；已有内容保留，稍后重试。"
		return

	var data = JSON.parse_string(body.get_string_from_utf8())

	if code != 200:
		var detail := ""
		if data is Dictionary:
			detail = str(data.get("error", ""))
		_status.text = "读取会面失败：HTTP %d。%s" % [code, detail]
		return

	if not data is Dictionary or not data.has("negotiation"):
		_status.text = "接口没有返回有效的会面数据。"
		return

	var session = data["negotiation"]

	if session == null:
		_status.text = "目前没有会面记录。"
		_text.text = ""
		return

	if not session is Dictionary:
		_status.text = "会面数据格式不正确。"
		return

	var returned_id := str(session.get("id", ""))
	if returned_id.is_empty():
		_status.text = "会面数据缺少编号。"
		return

	if not _session_id.is_empty() and returned_id != _session_id:
		_status.text = "接口返回了另一场会面，未覆盖当前记录。"
		return

	_session_id = returned_id

	if not session.has("messages") or not session["messages"] is Array:
		_status.text = "接口只返回了状态，没有返回 messages；请检查后端版本。"
		return

	_show_session(session)
	snapshot_received.emit(session)


func _show_session(session: Dictionary) -> void:
	var state := str(session.get("status", ""))
	var descriptions := {
		"inviting": "正在邀请双方。",
		"travelling": "双方正在前往会面地点。",
		"discussing": "双方正在商量。",
		"agreed": "双方已经同意报价。",
		"blocked": "本次商量没有达成协议。",
		"cancelled": "本次会面已取消。",
		"failed": "本次协商处理失败。"
	}

	var status_text := str(descriptions.get(state, state))
	var reason := str(session.get("end_reason", ""))
	if not reason.is_empty():
		status_text += "\n" + reason

	var actor := str(session.get("next_actor_id", ""))
	if state == "discussing" and not actor.is_empty():
		status_text += "\n等待%s回应。" % _actor_name(actor)

	_status.text = status_text

	var messages: Array = session["messages"]
	var lines := PackedStringArray()

	for value in messages:
		if not value is Dictionary:
			continue

		var speech := str(value.get("speech", "")).strip_edges()
		if speech.is_empty():
			continue

		lines.append(
			_actor_name(str(value.get("actor", ""))) + "：" + speech
		)

		var issue := str(value.get("reason", "")).strip_edges()
		if not issue.is_empty() and issue != speech:
			lines.append("公开说明：" + issue)

	var content := "\n\n".join(lines)

	if content.is_empty():
		if state in ["inviting", "travelling"]:
			content = "双方尚未开始当面交谈。"
		elif state == "discussing":
			content = "服务端暂未保存公开发言，正在等待更新。"
		else:
			content = "这场会面没有可显示的公开发言。"

	# 内容没有变化时，不反复重置文本和滚动位置。
	if _text.text != content:
		_text.text = content


func _actor_name(actor: String) -> String:
	return str({
		"merchant": "马丁",
		"blacksmith": "罗兰",
		"player-1": "你",
		"game_service": "会面"
	}.get(actor, actor))


func _exit_tree() -> void:
	_stop_polling()