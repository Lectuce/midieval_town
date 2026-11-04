extends VBoxContainer

var _base_url := ""
var _card: Dictionary = {}
var _allowed := false
var _busy := false

# 每份报价分别记录会话，切换报价不会串状态。
var _sessions: Dictionary = {}
var _request_ids: Dictionary = {}

var _button: Button
var _status: Label
var _http: HTTPRequest
var _timer: Timer

# 发出请求时固定上下文，避免等待期间切换报价造成串卡。
var _operation := ""
var _pending_key := ""
var _pending_proposal := ""


func _ready() -> void:
	_button = Button.new()
	_button.text = "邀请双方当面商量"
	_button.pressed.connect(_invite)
	add_child(_button)

	_status = Label.new()
	_status.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	_status.add_theme_font_size_override("font_size", 16)
	add_child(_status)

	_http = HTTPRequest.new()
	_http.timeout = 15.0
	_http.body_size_limit = 65536
	_http.request_completed.connect(_on_response)
	add_child(_http)

	_timer = Timer.new()
	_timer.wait_time = 1.5
	_timer.timeout.connect(_poll)
	add_child(_timer)
	_timer.start()

	_render()


func configure(base_url: String, card: Dictionary, allowed: bool) -> void:
	_base_url = base_url.trim_suffix("/")
	_card = card.duplicate(true)
	_allowed = allowed
	_render()


func _key() -> String:
	return _base_url + "|" + str(_card.get("id", ""))


func _eligible() -> bool:
	return (
		not _card.is_empty()
		and str(_card.get("status", "")) == "proposed"
		and bool(_card.get("worker_confirmed", false))
		and not bool(_card.get("payer_confirmed", false))
		and str(_card.get("payer_npc_id", "")) == "merchant"
		and str(_card.get("worker_npc_id", "")) == "blacksmith"
	)


func _render() -> void:
	if not is_instance_valid(_button):
		return

	var session: Dictionary = _sessions.get(_key(), {})
	var has_session := not str(session.get("id", "")).is_empty()

	_button.text = (
		"查看会面状态" if has_session
		else "邀请双方当面商量"
	)
	_button.disabled = (
		_busy or not _allowed
		or (not has_session and not _eligible())
	)

	if not has_session:
		_status.text = "发出邀请后，由马丁和罗兰决定是否赴约。"
		return

	var state := str(session.get("status", ""))
	var state_text := {
		"inviting": "正在等待双方回应。",
		"travelling": "双方愿意赴约，正在前往会面地点。",
		"discussing": "双方已到场，可以开始商量。",
		"agreed": "双方已经达成协议。",
		"blocked": "本次会面未能继续。",
		"cancelled": "本次会面已取消。",
		"failed": "本次会面处理失败。"
	}
	var lines := PackedStringArray([
		str(state_text.get(state, "正在查询会面状态。"))
	])

	var participants = session.get("participants", [])
	if participants is Array:
		for value in participants:
			if not value is Dictionary:
				continue

			var npc_id := str(value.get("npc_id", ""))
			var npc_name := "马丁" if npc_id == "merchant" else "罗兰"
			var invite := str(value.get("invite_status", "pending"))
			var arrival := str(value.get("arrival_status", "not_started"))

			var description := "等待回应"
			if invite == "declined":
				description = "拒绝赴约"
			elif invite == "accepted":
				description = "已接受邀请，尚未出发"
				if arrival == "travelling":
					description = "正在赶来"
				elif arrival == "arrived":
					description = "已到场"

			lines.append(npc_name + "：" + description)

	_status.text = "\n".join(lines)


func _invite() -> void:
	if _busy or not _allowed:
		return

	var key := _key()
	var session: Dictionary = _sessions.get(key, {})
	if not str(session.get("id", "")).is_empty():
		_poll()
		return

	if not _eligible():
		return

	# 同一报价版本的网络重试复用 request_id。
	var revision := int(_card.get("revision", 0))
	var request_key := key + "|" + str(revision)
	if not _request_ids.has(request_key):
		_request_ids[request_key] = (
			"meeting-" + Crypto.new().generate_random_bytes(16).hex_encode()
		)

	_send(
		"create",
		"/api/negotiations",
		HTTPClient.METHOD_POST,
		JSON.stringify({
			"request_id": _request_ids[request_key],
			"proposal_id": str(_card.get("id", "")),
			"expected_revision": revision
		})
	)


func _poll() -> void:
	if _busy or not is_visible_in_tree():
		return

	var session: Dictionary = _sessions.get(_key(), {})
	var session_id := str(session.get("id", ""))
	if session_id.is_empty():
		return

	_send(
		"get",
		"/api/negotiations/" + session_id.uri_encode(),
		HTTPClient.METHOD_GET
	)


func _send(
	operation: String,
	path: String,
	method: int,
	body: String = ""
) -> void:
	_busy = true
	_operation = operation
	_pending_key = _key()
	_pending_proposal = str(_card.get("id", ""))
	_render()

	if operation == "create":
		_status.text = "正在发送邀请…"

	var err := _http.request(
		_base_url + path,
		PackedStringArray([
			"Accept: application/json",
			"Content-Type: application/json",
			"Cache-Control: no-cache"
		]),
		method,
		body
	)

	if err != OK:
		_busy = false
		_render()
		_status.text = "请求未能发出，请稍后重试。"


func _on_response(
	result: int,
	code: int,
	_headers: PackedStringArray,
	body: PackedByteArray
) -> void:
	_busy = false
	var data = JSON.parse_string(body.get_string_from_utf8())
	var error_text := ""

	if result != HTTPRequest.RESULT_SUCCESS:
		error_text = (
			"邀请结果尚未确认，请再次点击查询或重试。"
			if _operation == "create"
			else "暂时无法查询，稍后自动重试。"
		)
	elif code != 200 or not data is Dictionary:
		error_text = (
			str(data.get("error", "请求失败。"))
			if data is Dictionary else "服务器返回的数据不正确。"
		)
	else:
		var session = data.get("negotiation")
		if (
			not session is Dictionary
			or str(session.get("id", "")).is_empty()
			or str(session.get("current_proposal_id", "")) != _pending_proposal
		):
			error_text = "会面数据不完整，请重新查询。"
		else:
			_sessions[_pending_key] = session.duplicate(true)

	_render()

	if not error_text.is_empty() and _pending_key == _key():
		_status.text = error_text


func _exit_tree() -> void:
	if is_instance_valid(_http):
		_http.cancel_request()