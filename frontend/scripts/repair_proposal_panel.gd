extends VBoxContainer

var busy := false
var _chat_waiting := false
var _opened := false
var _npc_id := ""
var _base_url := ""
var _cards: Array = []

var _request: HTTPRequest
var _generation := 0

var _selector: OptionButton
var _details: Label
var _notice: Label
var _deliver_button: Button
var _refresh_button: Button


func _ready() -> void:
	add_theme_constant_override("separation", 6)

	var top := HBoxContainer.new()
	add_child(top)

	_selector = OptionButton.new()
	_selector.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	_selector.item_selected.connect(_on_selected)
	top.add_child(_selector)

	_refresh_button = Button.new()
	_refresh_button.text = "刷新报价"
	_refresh_button.pressed.connect(refresh)
	top.add_child(_refresh_button)

	_details = Label.new()
	_details.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	_details.add_theme_font_size_override("font_size", 16)
	add_child(_details)

	var bottom := HBoxContainer.new()
	add_child(bottom)

	_notice = Label.new()
	_notice.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	_notice.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	_notice.add_theme_font_size_override("font_size", 15)
	bottom.add_child(_notice)

	_deliver_button = Button.new()
	_deliver_button.text = "转交马丁"
	_deliver_button.pressed.connect(_deliver)
	bottom.add_child(_deliver_button)

	hide()


func open_for(npc_id: String, base_url: String) -> void:
	_cancel_request()
	_npc_id = npc_id
	_base_url = base_url.trim_suffix("/")
	_opened = npc_id in ["blacksmith", "merchant"]
	visible = _opened
	_cards.clear()
	_selector.clear()
	_notice.text = ""
	_render()

	if _opened:
		refresh()


func close_panel() -> void:
	_opened = false
	_cancel_request()
	hide()


func set_chat_waiting(value: bool) -> void:
	_chat_waiting = value
	_render()


func show_cards(cards: Array) -> void:
	if not _opened:
		return

	_cards.clear()
	_selector.clear()

	for value in cards:
		if value is Dictionary and not str(value.get("id", "")).is_empty():
			_cards.append(value.duplicate(true))

	for i in range(_cards.size()):
		var card: Dictionary = _cards[i]
		_selector.add_item(
			"维修报价 %d：工钱 %d，工时 %d 分钟" % [
				i + 1,
				int(card.get("price_coins", 0)),
				int(card.get("duration_minutes", 0))
			]
		)

	if not _cards.is_empty():
		_selector.select(0)

	_render()


func _on_selected(_index: int) -> void:
	_notice.text = ""
	_render()


func _selected_card() -> Dictionary:
	var index := _selector.selected
	if index < 0 or index >= _cards.size():
		return {}
	return _cards[index]


func _render() -> void:
	if not is_instance_valid(_selector):
		return

	_refresh_button.disabled = busy or _chat_waiting
	_selector.disabled = busy or _chat_waiting or _cards.is_empty()

	var card := _selected_card()
	_deliver_button.disabled = true
	_deliver_button.text = "转交马丁"

	if card.is_empty():
		_details.text = "暂无维修报价。"
		return

	var status := str(card.get("status", ""))
	var delivered := bool(card.get("delivered", false))
	var state_text := "罗兰已确认，尚未转交"

	if status == "cancelled":
		state_text = "报价已取消"
	elif status == "executed":
		state_text = "已启动维修"
	elif status == "confirmed":
		state_text = "双方已确认，尚未启动维修"
	elif delivered:
		state_text = "马丁已收到，尚未确认"

	_details.text = (
		"罗兰的维修报价｜付款人：马丁\n"
		+ "工钱：%d 枚钱币｜罗兰提供材料：%d 份｜工时：%d 分钟\n"
		+ "状态：%s"
	) % [
		int(card.get("price_coins", 0)),
		int(card.get("material_count", 0)),
		int(card.get("duration_minutes", 0)),
		state_text
	]

	if delivered:
		_deliver_button.text = "已转交"
	elif _npc_id != "merchant":
		_deliver_button.text = "与马丁交谈时转交"
	else:
		_deliver_button.disabled = (
			busy
			or _chat_waiting
			or status != "proposed"
			or not bool(card.get("worker_confirmed", false))
			or str(card.get("payer_npc_id", "")) != "merchant"
		)


func refresh() -> void:
	if not _opened or busy or _chat_waiting:
		return

	_notice.text = "正在查询报价…"
	_start_request(
		HTTPClient.METHOD_GET,
		"/api/player/repair-proposals"
	)


func _deliver() -> void:
	if not _opened or busy or _chat_waiting or _npc_id != "merchant":
		return

	var card := _selected_card()
	if card.is_empty() or bool(card.get("delivered", false)):
		return

	var proposal_id := str(card.get("id", ""))
	_notice.text = "正在转交报价…"

	_start_request(
		HTTPClient.METHOD_POST,
		"/api/npcs/merchant/repair-proposals/"
			+ proposal_id.uri_encode() + "/deliver",
		proposal_id
	)


func _start_request(
	method: int,
	path: String,
	proposal_id: String = ""
) -> void:
	_cancel_request()

	busy = true
	_render()

	_request = HTTPRequest.new()
	_request.timeout = 15.0
	_request.body_size_limit = 131072
	add_child(_request)

	_request.request_completed.connect(
		_on_response.bind(_generation, proposal_id)
	)

	var err := _request.request(
		_base_url + path,
		PackedStringArray([
			"Accept: application/json",
			"Cache-Control: no-cache"
		]),
		method
	)

	if err != OK:
		_cancel_request()
		_notice.text = "无法发起请求，请检查网络后刷新。"
		_render()


func _on_response(
	result: int,
	code: int,
	_headers: PackedStringArray,
	body: PackedByteArray,
	generation: int,
	proposal_id: String
) -> void:
	if generation != _generation or not _opened:
		return

	_cancel_request()

	var data = JSON.parse_string(body.get_string_from_utf8())

	if result != HTTPRequest.RESULT_SUCCESS:
		_notice.text = (
			"转交结果尚未确认，请刷新报价查看。"
			if not proposal_id.is_empty()
			else "报价查询失败，请刷新重试。"
		)
		_render()
		return

	if code != 200 or not data is Dictionary:
		_notice.text = (
			str(data.get("error", "请求失败，请刷新确认状态。"))
			if data is Dictionary
			else "返回数据异常，请刷新确认状态。"
		)
		_render()
		return

	if proposal_id.is_empty():
		var cards = data.get("repair_proposals")
		if not cards is Array:
			_notice.text = "报价数据不完整，请刷新重试。"
			_render()
			return

		show_cards(cards)
		_notice.text = ""
		return

	var proposal = data.get("proposal")
	if not proposal is Dictionary or str(proposal.get("id", "")) != proposal_id:
		_notice.text = "转交结果不完整，请刷新确认状态。"
		_render()
		return

	# 只有对应报价的转交接口返回成功，才标记已送达。
	for i in range(_cards.size()):
		if str(_cards[i].get("id", "")) == proposal_id:
			var updated: Dictionary = proposal.duplicate(true)
			updated["delivered"] = true
			_cards[i] = updated
			break

	_notice.text = "马丁已收到报价，尚未作出答复。"
	_render()


func _cancel_request() -> void:
	_generation += 1
	busy = false

	if is_instance_valid(_request):
		_request.cancel_request()
		_request.queue_free()
		_request = null


func _exit_tree() -> void:
	_cancel_request()