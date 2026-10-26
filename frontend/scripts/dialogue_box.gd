extends CanvasLayer

const MAX_MESSAGES := 60
const QUEST_ID := "missing_iron_cart"
const INPUT_HINT := "Enter：发送    Esc：结束对话    输入时 E 和方向键用于编辑文字"
var stream := preload("res://scripts/web_dialogue_stream.gd").new()
var repair_ui := preload("res://scripts/repair_proposal_panel.gd").new()
var waiting: bool = false
var request_id: String = ""
var reply_index: int = -1
var reply_text: String = ""
var reply_name: String = ""
var active: bool = false
var speaker: Node2D
var lines: Array[String] = []
var line_index: int = 0
var panel: PanelContainer
var title_label: Label
var body_label: RichTextLabel
var footer_label: Label
var input_field: LineEdit
var next_button: Button
var send_button: Button
var close_button: Button
var messages: Array[String] = []
var quest_panel: HBoxContainer
var quest_label: Label
var quest_button: Button
var quest_request: HTTPRequest
var quest_generation: int = 0
var quest_status: String = ""
var quest_busy := false
var quest_position: Dictionary = {}
var quest_expected_revision := 0
var quest_completion_message := ""
var quest_reconcile_reason := ""

func _ready() -> void:
	add_child(stream)
	stream.event_received.connect(_on_stream_event)
	layer = 20
	panel = PanelContainer.new()
	add_child(panel)
	panel.set_anchors_and_offsets_preset(Control.PRESET_BOTTOM_WIDE)
	panel.anchor_left = 0.08
	panel.anchor_right = 0.92
	panel.offset_top = -330
	panel.offset_bottom = -24
	var style := StyleBoxFlat.new()
	style.bg_color = Color("202635")
	style.border_color = Color("b2c0cd")
	style.set_border_width_all(2)
	style.content_margin_left = 24
	style.content_margin_right = 24
	style.content_margin_top = 16
	style.content_margin_bottom = 16
	panel.add_theme_stylebox_override("panel", style)
	var column := VBoxContainer.new()
	column.add_theme_constant_override("separation", 10)
	panel.add_child(column)
	var header := HBoxContainer.new()
	column.add_child(header)
	title_label = Label.new()
	title_label.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	title_label.add_theme_font_size_override("font_size", 22)
	title_label.add_theme_color_override("font_color", Color("edcf89"))
	header.add_child(title_label)
	next_button = Button.new()
	next_button.text = "听下一句"
	next_button.pressed.connect(advance)
	header.add_child(next_button)
	close_button = Button.new()
	close_button.text = "结束对话"
	close_button.pressed.connect(close)
	header.add_child(close_button)
	body_label = RichTextLabel.new()
	body_label.bbcode_enabled = false
	body_label.add_theme_font_size_override("normal_font_size", 20)
	body_label.custom_minimum_size.y = 140
	body_label.size_flags_vertical = Control.SIZE_EXPAND_FILL
	body_label.scroll_following = true
	body_label.selection_enabled = true
	column.add_child(body_label)
	quest_panel = HBoxContainer.new()
	column.add_child(quest_panel)
	quest_label = Label.new()
	quest_label.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	quest_label.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	quest_label.add_theme_font_size_override("font_size", 16)
	quest_panel.add_child(quest_label)
	quest_button = Button.new()
	quest_button.text = "接受委托"
	quest_button.pressed.connect(_on_quest_pressed)
	quest_panel.add_child(quest_button)
	quest_panel.hide()
	column.add_child(repair_ui)
	var row := HBoxContainer.new()
	column.add_child(row)
	input_field = LineEdit.new()
	input_field.placeholder_text = "输入你想说的话，按 Enter 发送…"
	input_field.max_length = 256
	input_field.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	input_field.custom_minimum_size.y = 40
	input_field.add_theme_font_size_override("font_size", 20)
	input_field.text_submitted.connect(send_message)
	row.add_child(input_field)
	send_button = Button.new()
	send_button.text = "发送"
	send_button.custom_minimum_size.x = 80
	send_button.pressed.connect(func(): send_message(input_field.text))
	row.add_child(send_button)
	footer_label = Label.new()
	footer_label.text = INPUT_HINT
	footer_label.add_theme_font_size_override("font_size", 15)
	footer_label.add_theme_color_override("font_color", Color("b2c0cd"))
	column.add_child(footer_label)
	panel.hide()

func _input(event: InputEvent) -> void:
	# Let Escape dismiss an IME candidate first, rather than closing the dialogue.
	if active and event.is_action_pressed("ui_cancel") and not is_ime_composing():
		close()
		get_viewport().set_input_as_handled()

func is_ime_composing() -> bool:
	return DisplayServer.has_feature(DisplayServer.FEATURE_IME) and not DisplayServer.ime_get_text().is_empty()

func open(npc: Node2D) -> void:
	_cancel_request()
	_cancel_quest_request()
	speaker = npc
	lines.assign(npc.dialogue_lines)
	if lines.is_empty():
		lines.append("你好。")
	line_index = 0
	messages.clear()
	active = true
	title_label.text = npc.display_name
	input_field.clear()
	append_message(speaker.display_name, lines[0])
	next_button.disabled = lines.size() <= 1
	footer_label.text = INPUT_HINT
	panel.show()
	quest_panel.visible = str(npc.npc_id) == "merchant"
	var has_repair_panel := str(npc.npc_id) in ["blacksmith", "merchant"]
	panel.offset_top = -600 if has_repair_panel else -330
	repair_ui.open_for(str(npc.npc_id), _quest_base_url())
	quest_status = ""
	quest_completion_message = ""
	quest_reconcile_reason = ""
	if quest_panel.visible:
		_request_quest(false)
	input_field.grab_focus.call_deferred()

func _merchant_dialogue_active() -> bool:
	return active and is_instance_valid(speaker) and str(speaker.npc_id) == "merchant"

func _on_quest_pressed() -> void:
	if quest_busy or not _merchant_dialogue_active() or quest_status == "completed":
		return
	if quest_status == "active":
		var world = get_parent()
		if world.current_floor != world.Floor.OUTSIDE or world.player.global_position.distance_to(speaker.global_position) > speaker.talk_distance:
			_quest_failed("请靠近商人后再提交。", true)
			return
		var position_in_town: Vector2 = world.to_local(world.player.global_position)
		quest_position = {"position": {"x": position_in_town.x, "y": position_in_town.y}, "floor": int(world.current_floor)}
		quest_reconcile_reason = ""
		_send_quest_step("player")
	else:
		_request_quest(quest_status == "not_started")

func _request_quest(accept: bool) -> void:
	if quest_busy or not _merchant_dialogue_active():
		return
	_send_quest_step("accept" if accept else "query")

func _quest_base_url() -> String:
	var base := str(ProjectSettings.get_setting("quest/base_url", "")).trim_suffix("/")
	if base.is_empty():
		base = str(JavaScriptBridge.eval("window.location.origin")) if OS.has_feature("web") else "http://127.0.0.1:8080"
	return base

func _send_quest_step(step: String) -> void:
	if not _merchant_dialogue_active():
		return
	_cancel_quest_request()
	quest_busy = true
	quest_button.disabled = true
	quest_button.text = "确认中…" if step == "reconcile" else "请求中…"
	quest_label.text = "正在查询任务状态…" if step in ["query", "reconcile"] else "正在接受委托…" if step == "accept" else "正在同步位置并提交凭证…"
	var base := _quest_base_url()
	var url := base + "/api/quests/" + QUEST_ID
	var method := HTTPClient.METHOD_GET
	var body := ""
	if step in ["player", "position", "conflict_player"]:
		url = str(get_parent().player_api_url).trim_suffix("/")
		if url.begins_with("/"):
			url = base + url
	if step == "position":
		url += "/position"
		method = HTTPClient.METHOD_PUT
		var payload := quest_position.duplicate(true)
		payload["expected_revision"] = quest_expected_revision
		body = JSON.stringify(payload)
	elif step in ["accept", "complete"]:
		url += "/" + step
		method = HTTPClient.METHOD_POST
	quest_request = HTTPRequest.new()
	quest_request.timeout = 15.0
	quest_request.body_size_limit = 65536
	add_child(quest_request)
	quest_request.request_completed.connect(_on_quest_step.bind(quest_generation, step))
	var headers := PackedStringArray(["Accept: application/json", "Cache-Control: no-cache"])
	if not body.is_empty():
		headers.append("Content-Type: application/json")
	var err := quest_request.request(url, headers, method, body)
	if err != OK:
		_cancel_quest_request()
		_quest_failed("无法发起请求，请检查网络后重试查询。")

func _on_quest_step(result: int, code: int, _headers: PackedStringArray, body: PackedByteArray, generation: int, step: String) -> void:
	if generation != quest_generation or not _merchant_dialogue_active():
		return
	_cancel_quest_request()
	var data = JSON.parse_string(body.get_string_from_utf8())
	if result != HTTPRequest.RESULT_SUCCESS:
		if step in ["complete", "accept"]:
			_reconcile_quest("提交响应未收到，正在确认服务端状态。")
		else:
			_quest_failed("请求未完成，请重试查询任务状态。")
		return
	if code != 200:
		var reason := str(data.get("error", "请求失败（HTTP %d）" % code)) if data is Dictionary else "请求失败（HTTP %d）" % code
		if step == "position" and code == 409:
			quest_reconcile_reason = reason
			_send_quest_step("conflict_player")
		elif step == "complete" and code >= 500:
			_reconcile_quest(reason)
		else:
			_quest_failed(reason, step == "complete" and code in [400, 403])
		return
	if not data is Dictionary:
		if step == "complete":
			_reconcile_quest("结算响应格式错误，需确认是否已经完成。")
		else:
			_quest_failed("响应格式错误，请重试查询。")
		return
	if step in ["player", "position", "conflict_player"]:
		var revision = data.get("revision")
		if not (revision is int or revision is float) or revision < 1 or revision != floor(revision):
			_quest_failed("玩家版本无效，请重试查询。")
			return
		if step == "player":
			quest_expected_revision = int(revision)
			_send_quest_step("position")
		elif step == "conflict_player":
			get_parent().latest_player = data
			_quest_failed(quest_reconcile_reason + " 已重新查询位置，请重试。", true)
		else:
			var saved_position = data.get("position")
			if not saved_position is Dictionary:
				_quest_failed("服务器返回的位置格式错误。", true)
				return

			var saved_x = saved_position.get("x")
			var saved_y = saved_position.get("y")
			if not (saved_x is int or saved_x is float) \
					or not (saved_y is int or saved_y is float):
				_quest_failed("服务器返回的坐标格式错误。", true)
				return

			var x := float(saved_x)
			var y := float(saved_y)

			# 允许 JSON 转换产生的微小误差。
			var position_matches := (
				is_finite(x)
				and is_finite(y)
				and absf(x - float(quest_position.position.x)) <= 0.001
				and absf(y - float(quest_position.position.y)) <= 0.001
			)

			if not position_matches \
					or data.get("floor") != quest_position.floor \
					or revision < quest_expected_revision:
				_quest_failed("服务器确认的位置不一致，请重新提交。", true)
				return

			get_parent().latest_player = data
			_send_quest_step("complete")
		return
	var quest = data.get("quest") if step == "complete" else data
	if not quest is Dictionary or quest.get("quest_id") != QUEST_ID or not quest.get("status") in ["not_started", "active", "completed"]:
		if step == "complete":
			_reconcile_quest("结算响应不完整，需确认任务状态。")
		else:
			_quest_failed("返回的任务或状态无效，请重试查询。")
		return
	if step == "complete" and quest.status != "completed":
		_reconcile_quest("尚未确认任务完成。")
		return
	_apply_quest_state(quest)
	if quest_status == "completed":
		quest_completion_message = str(data.get("message", "寻找铁矿货车：委托已完成。")) if step == "complete" else "寻找铁矿货车：委托已完成。"
		quest_label.text = quest_completion_message
		_refresh_settled_inventory()
	elif step == "reconcile":
		quest_label.text = quest_reconcile_reason + " 当前任务尚未完成，可再次操作。"

func _reconcile_quest(reason: String) -> void:
	quest_reconcile_reason = reason
	_send_quest_step("reconcile")

func _apply_quest_state(data: Dictionary) -> void:
	quest_status = str(data.status)
	quest_button.disabled = quest_status == "completed"
	match quest_status:
		"not_started":
			quest_label.text = str(data.get("title", "寻找铁矿货车")) + "\n" + str(data.get("description", ""))
			quest_button.text = "接受委托"
		"active":
			quest_label.text = "寻找铁矿货车：取得货物凭证后交给商人。"
			quest_button.text = "提交凭证"
		"completed":
			quest_label.text = "寻找铁矿货车：委托已完成。"
			quest_button.text = "已完成"

func _refresh_settled_inventory() -> void:
	var inventory = get_parent().inventory
	if not inventory.inventory_failed.is_connected(_on_settlement_inventory_failed):
		inventory.inventory_failed.connect(_on_settlement_inventory_failed)
	if not inventory.inventory_loaded.is_connected(_on_settlement_inventory_loaded):
		inventory.inventory_loaded.connect(_on_settlement_inventory_loaded)
	inventory.refresh(false, "结算成功")

func _on_settlement_inventory_failed(_reason: String) -> void:
	if _merchant_dialogue_active() and quest_status == "completed":
		quest_label.text = quest_completion_message + "\n结算已完成，背包刷新失败。结束对话后按 B，点击刷新重试。"

func _on_settlement_inventory_loaded() -> void:
	if _merchant_dialogue_active() and quest_status == "completed":
		quest_label.text = quest_completion_message + "\n背包已更新。"

func _quest_failed(message: String, retain_active: bool = false) -> void:
	quest_busy = false
	quest_status = "active" if retain_active else ""
	quest_label.text = message
	quest_button.text = "提交凭证" if retain_active else "重试查询"
	quest_button.disabled = false

func _cancel_quest_request() -> void:
	quest_generation += 1
	quest_busy = false
	if is_instance_valid(quest_request):
		quest_request.cancel_request()
		quest_request.queue_free()
		quest_request = null

func append_message(who: String, message: String) -> void:
	messages.append(who + "：" + message)
	if messages.size() > MAX_MESSAGES:
		messages.pop_front()
	_render_messages()

func _render_messages() -> void:
	body_label.clear()
	# add_text treats all player input as plain text, including BBCode-like text.
	body_label.add_text("\n\n".join(messages))

func send_message(text: String) -> void:
	if not active or waiting or not is_instance_valid(speaker):
		return
	if repair_ui.busy:
		footer_label.text = "请等待报价请求完成。"
		return
	var message := text.strip_edges().left(input_field.max_length)
	if message.is_empty() or is_ime_composing():
		return
	var npc_id := str(speaker.npc_id).strip_edges()
	if npc_id.is_empty():
		footer_label.text = "这位 NPC 尚未设置 Npc Id，暂时无法发送。"
		return
	append_message("你", message)
	input_field.clear()
	reply_name = speaker.display_name
	reply_text = ""
	append_message(reply_name, "正在思考…")
	reply_index = messages.size() - 1
	request_id = Crypto.new().generate_random_bytes(16).hex_encode()
	_set_waiting(true)
	footer_label.text = "正在等待回复…    Esc：取消并结束对话"
	stream.start(request_id, npc_id, message)

func _on_stream_event(id: String, event: String, data: Dictionary) -> void:
	if not active or not waiting or id != request_id:
		return
	if not is_instance_valid(speaker):
		close()
		return
	match event:
		"delta":
			if not data.get("text") is String:
				_fail_request("回复片段格式错误。")
				return
			reply_text += data["text"]
			_update_reply(reply_text)
			footer_label.text = "正在回复…    Esc：取消并结束对话"
		"done":
			if not data.get("reply") is String:
				_fail_request("完整回复格式错误。")
				return
			reply_text = data["reply"]
			_update_reply(reply_text)
			var cards = data.get("repair_proposals")
			if cards is Array:
				repair_ui.show_cards(cards)
			_finish_request()
			footer_label.text = INPUT_HINT
			if not cards is Array:
				footer_label.text = "回复已收到，但缺少报价数据，请刷新报价。"
		"error":
			_fail_request(str(data.get("message", data.get("error", "网络请求失败。"))))

func _update_reply(text: String) -> void:
	if reply_index >= 0 and reply_index < messages.size():
		messages[reply_index] = reply_name + "：" + text
		_render_messages()

func _fail_request(reason: String) -> void:
	_update_reply(reply_text + ("\n" if not reply_text.is_empty() else "") +
		"【本轮未完成】" + reason)
	_finish_request()
	footer_label.text = "本轮未完成，可重新发送或按 Esc 结束对话。"

func _finish_request() -> void:
	# Invalidate before aborting; queued callbacks must not touch the next turn.
	request_id = ""
	stream.cancel()
	_set_waiting(false)
	input_field.grab_focus()

func _set_waiting(value: bool) -> void:
	waiting = value
	send_button.disabled = value
	send_button.text = "回复中…" if value else "发送"
	input_field.editable = not value
	next_button.disabled = value or line_index + 1 >= lines.size()
	repair_ui.set_chat_waiting(value)

func _cancel_request() -> void:
	request_id = ""
	stream.cancel()
	reply_index = -1
	reply_text = ""
	_set_waiting(false)

func advance() -> void:
	if not active or waiting or line_index + 1 >= lines.size():
		return
	line_index += 1
	append_message(speaker.display_name, lines[line_index])
	next_button.disabled = line_index + 1 >= lines.size()
	input_field.grab_focus()

func close() -> void:
	_cancel_request()
	_cancel_quest_request()
	repair_ui.close_panel()
	active = false
	speaker = null
	input_field.release_focus()
	panel.hide()

func _exit_tree() -> void:
	_cancel_quest_request()
	repair_ui.close_panel()
	request_id = ""
	stream.cancel()
