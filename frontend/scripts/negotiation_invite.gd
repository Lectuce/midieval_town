extends VBoxContainer

signal proposal_changed(proposal: Dictionary)

var _base_url := ""
var _card: Dictionary = {}
var _session: Dictionary = {}
var _allowed := false
var _busy := false
var _loaded := false

var _operation := ""
var _pending_session := ""
var _request_ids: Dictionary = {}
var _last_proposal_stamp := ""

var _message_id := ""
var _message_text := ""
var _message_session := ""
var _position_snapshot: Dictionary = {}
var _world: Node

var _button: Button
var _status: Label
var _transcript: RichTextLabel
var _input: LineEdit
var _send_button: Button
var _cancel_button: Button
var _notice: Label
var _http: HTTPRequest
var _timer: Timer


func _ready() -> void:
	_button = Button.new()
	_button.pressed.connect(_invite)
	add_child(_button)

	_status = Label.new()
	_status.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	add_child(_status)

	_transcript = RichTextLabel.new()
	_transcript.bbcode_enabled = false
	_transcript.custom_minimum_size.y = 150
	_transcript.scroll_following = true
	add_child(_transcript)

	var row := HBoxContainer.new()
	add_child(row)

	_input = LineEdit.new()
	_input.placeholder_text = "在发言间隙插话，不会替你承诺金币或材料"
	_input.max_length = 256
	_input.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	_input.text_submitted.connect(func(_text: String): _send_message())
	row.add_child(_input)

	_send_button = Button.new()
	_send_button.text = "插话"
	_send_button.pressed.connect(_send_message)
	row.add_child(_send_button)

	_cancel_button = Button.new()
	_cancel_button.text = "结束本次协商"
	_cancel_button.pressed.connect(_cancel)
	add_child(_cancel_button)

	_notice = Label.new()
	_notice.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	add_child(_notice)

	_http = HTTPRequest.new()
	_http.timeout = 15.0
	_http.body_size_limit = 262144
	_http.request_completed.connect(_on_response)
	add_child(_http)

	_timer = Timer.new()
	_timer.wait_time = 1.5
	_timer.timeout.connect(_poll)
	add_child(_timer)
	_timer.start()

	_render()


func configure(base_url: String, card: Dictionary, allowed: bool) -> void:
	var next_base := base_url.trim_suffix("/")
	if _base_url != next_base:
		_loaded = false
		_session.clear()
		_last_proposal_stamp = ""
	_base_url = next_base
	_card = card.duplicate(true)
	_allowed = allowed
	_render()


func _active() -> bool:
	return str(_session.get("status", "")) in [
		"inviting", "travelling", "discussing"
	]


func _eligible() -> bool:
	return (
		str(_card.get("status", "")) == "proposed"
		and bool(_card.get("worker_confirmed", false))
		and not bool(_card.get("payer_confirmed", false))
		and str(_card.get("payer_npc_id", "")) == "merchant"
		and str(_card.get("worker_npc_id", "")) == "blacksmith"
	)


func _actor_name(actor: String) -> String:
	return str({
		"merchant": "马丁",
		"blacksmith": "罗兰",
		"player-1": "你",
		"game_service": "会面"
	}.get(actor, actor))


func _render() -> void:
	if not is_instance_valid(_button):
		return

	_button.text = "查看会面状态" if _active() else "邀请双方当面商量"
	_button.disabled = (
		_busy or not _allowed or not _loaded
		or (not _active() and not _eligible())
	)

	var state := str(_session.get("status", ""))
	var descriptions := {
		"inviting": "正在等待双方回应。",
		"travelling": "双方愿意赴约，正在前往会面地点。",
		"discussing": "双方正在商量。",
		"agreed": "双方已同意报价，尚未代表已经开工。",
		"blocked": "本次商量没有继续。",
		"cancelled": "本次会面已结束。",
		"failed": "本次协商处理失败。"
	}
	var lines := PackedStringArray()

	if _session.is_empty():
		lines.append("发出邀请后，由马丁和罗兰决定是否赴约。")
	else:
		lines.append(str(descriptions.get(state, "正在查询会面。")))

		var next_actor := str(_session.get("next_actor_id", ""))
		if state == "discussing":
			lines.append(
				"等待%s回应。" % _actor_name(next_actor)
				if not next_actor.is_empty()
				else "正在安排下一次交谈。"
			)

		var reason := str(_session.get("end_reason", ""))
		if not reason.is_empty():
			lines.append(reason)

		var participants = _session.get("participants", [])
		if participants is Array:
			for value in participants:
				if not value is Dictionary:
					continue
				var description := "等待邀请回应"
				if value.get("invite_status") == "declined":
					description = "拒绝赴约"
				elif value.get("invite_status") == "accepted":
					description = str({
						"not_started": "愿意赴约",
						"travelling": "正在赶来",
						"arrived": "已到场"
					}.get(str(value.get("arrival_status")), "愿意赴约"))
				lines.append(
					_actor_name(str(value.get("npc_id"))) + "：" + description
				)

	_status.text = "\n".join(lines)

	var transcript := PackedStringArray()
	var messages = _session.get("messages", [])
	if messages is Array:
		for value in messages:
			if not value is Dictionary:
				continue
			transcript.append(
				_actor_name(str(value.get("actor", "")))
				+ "：" + str(value.get("speech", ""))
			)
			var issue := str(value.get("reason", ""))
			if not issue.is_empty():
				transcript.append("公开说明：" + issue)

	# 每次替换整个有序快照，不反复追加同一条发言。
	_transcript.text = "\n\n".join(transcript)

	var can_speak := state == "discussing" and _allowed and not _busy
	_input.editable = can_speak
	_send_button.disabled = not can_speak
	_cancel_button.disabled = _busy or not _allowed or not _active()


func _invite() -> void:
	if _busy or not _allowed:
		return
	if _active():
		_poll()
		return
	if not _eligible():
		return

	var key := (
		str(_card.get("id", "")) + "|"
		+ str(_card.get("revision", 0)) + "|"
		+ str(_session.get("id", ""))
	)
	if not _request_ids.has(key):
		_request_ids[key] = "meeting-" + Crypto.new().generate_random_bytes(16).hex_encode()

	_send("create", "/api/negotiations", HTTPClient.METHOD_POST, {
		"request_id": _request_ids[key],
		"proposal_id": str(_card.get("id", "")),
		"expected_revision": int(_card.get("revision", 0))
	})


func _poll() -> void:
	if _busy or not is_visible_in_tree() or _base_url.is_empty():
		return

	if not _loaded or _session.is_empty():
		_send("latest", "/api/player/negotiations/latest", HTTPClient.METHOD_GET)
		return

	_pending_session = str(_session.get("id", ""))
	_send(
		"get",
		"/api/negotiations/" + _pending_session.uri_encode(),
		HTTPClient.METHOD_GET
	)


func _send_message() -> void:
	if _busy or not _allowed or _session.get("status") != "discussing":
		return

	var text := _input.text.strip_edges()
	if text.is_empty():
		return

	_world = get_parent()
	while is_instance_valid(_world):
		if _world.get("player") != null and _world is Node2D:
			break
		_world = _world.get_parent()

	if not is_instance_valid(_world):
		_notice.text = "没有找到玩家所在的小镇节点，无法同步插话位置。"
		return

	var player = _world.get("player")
	if not is_instance_valid(player):
		_notice.text = "玩家尚未加载。"
		return

	var local_position: Vector2 = _world.to_local(player.global_position)
	_position_snapshot = {
		"position": {"x": local_position.x, "y": local_position.y},
		"floor": int(_world.get("current_floor"))
	}

	var session_id := str(_session.get("id", ""))
	if _message_id.is_empty() or _message_text != text or _message_session != session_id:
		_message_id = "speech-" + Crypto.new().generate_random_bytes(16).hex_encode()

	_message_text = text
	_message_session = session_id
	_notice.text = "正在确认你的位置并排队发言…"
	_send("player", "/api/player", HTTPClient.METHOD_GET)


func _cancel() -> void:
	if _busy or not _allowed or not _active():
		return
	_send(
		"cancel",
		"/api/negotiations/" + str(_session.get("id", "")).uri_encode() + "/cancel",
		HTTPClient.METHOD_POST
	)


func _send(operation: String, path: String, method: int, data: Dictionary = {}) -> void:
	_busy = true
	_operation = operation
	_render()

	var body := "" if method == HTTPClient.METHOD_GET else JSON.stringify(data)
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
		_notice.text = "请求未发出，可以重试。"
		_render()


func _accept_session(value: Dictionary) -> void:
	_session = value.duplicate(true)
	_loaded = true

	var proposal = value.get("proposal")
	if proposal is Dictionary and not proposal.is_empty():
		var stamp := (
			str(proposal.get("id", "")) + "|"
			+ str(proposal.get("revision", 0)) + "|"
			+ str(value.get("delivered", false))
		)
		if stamp != _last_proposal_stamp:
			_last_proposal_stamp = stamp
			var card: Dictionary = proposal.duplicate(true)
			card["delivered"] = bool(value.get("delivered", false))
			proposal_changed.emit(card)


func _on_response(
	result: int,
	code: int,
	_headers: PackedStringArray,
	body: PackedByteArray
) -> void:
	_busy = false
	var data = JSON.parse_string(body.get_string_from_utf8())

	if result != HTTPRequest.RESULT_SUCCESS or code != 200 or not data is Dictionary:
		_notice.text = (
			str(data.get("error", "请求没有确认成功，可以重试。"))
			if data is Dictionary
			else "请求没有确认成功，可以重试。"
		)
		_render()
		return

	match _operation:
		"latest":
			var value = data.get("negotiation")
			if value == null:
				_loaded = true
				_session.clear()
			elif value is Dictionary:
				_accept_session(value)
			else:
				_notice.text = "会面数据格式不正确。"

		"create", "get":
			var value = data.get("negotiation")
			if not value is Dictionary or str(value.get("id", "")).is_empty():
				_notice.text = "会面数据不完整。"
			elif _operation == "get" and str(value.get("id")) != _pending_session:
				_notice.text = "返回了另一场会面的数据。"
			else:
				_accept_session(value)
				_notice.text = ""

		"player":
			var revision := int(data.get("revision", 0))
			if revision < 1:
				_notice.text = "玩家版本不正确。"
			else:
				var request := _position_snapshot.duplicate(true)
				request["expected_revision"] = revision
				_send("position", "/api/player/position", HTTPClient.METHOD_PUT, request)
				return

		"position":
			var position = data.get("position")
			var target: Dictionary = _position_snapshot["position"]
			var valid := position is Dictionary
			if valid:
				valid = (
					absf(float(position.get("x", INF)) - float(target["x"])) < 0.001
					and absf(float(position.get("y", INF)) - float(target["y"])) < 0.001
					and int(data.get("floor", -1)) == int(_position_snapshot["floor"])
				)

			if not valid:
				_notice.text = "服务器确认的位置不一致，本次没有提交发言。"
			else:
				if is_instance_valid(_world):
					_world.set("latest_player", data)
				_send(
					"message",
					"/api/negotiations/" + _message_session.uri_encode() + "/messages",
					HTTPClient.METHOD_POST,
					{"request_id": _message_id, "message": _message_text}
				)
				return

		"message":
			_input.clear()
			_message_id = ""
			_notice.text = "发言已排队，将在下一次发言开始前送达。若协商先结束，则不再投递。"

		"cancel":
			_notice.text = "本次协商已结束。"
			_loaded = false

	_render()


func _exit_tree() -> void:
	if is_instance_valid(_http):
		_http.cancel_request()