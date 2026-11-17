extends CanvasLayer

signal claim_requested
signal inventory_loaded
signal inventory_failed(reason: String)

const ITEM_ART := {
	"shipment_receipt": {"icon": preload("res://assets/ui/receipt.svg"), "description": "在失踪货车现场取得，可交给商人核对货物。"},
	"coin": {"icon": preload("res://assets/ui/coin.svg"), "description": "完成委托获得的报酬。"},
}
var player_api_url := "/api/player"
var panel: PanelContainer
var grid: GridContainer
var details: Label
var status: Label
var refresh_button: Button
var claim_button: Button
var launcher: Button
var items: Array = []
var request: HTTPRequest
var generation := 0
var claim_refresh_pending := false
var refresh_success_context := ""
var selected_id := ""

func _ready() -> void:
	layer = 25
	launcher = Button.new()
	launcher.text = "背包  [B]"
	add_child(launcher)
	launcher.set_anchors_and_offsets_preset(Control.PRESET_TOP_RIGHT)
	launcher.offset_left = -150
	launcher.offset_right = -20
	launcher.offset_top = 16
	launcher.offset_bottom = 56
	launcher.pressed.connect(toggle)
	panel = PanelContainer.new()
	add_child(panel)
	var style := StyleBoxFlat.new()
	style.bg_color = Color("202635")
	style.border_color = Color("c6a76b")
	style.set_border_width_all(3)
	style.content_margin_left = 20
	style.content_margin_right = 20
	style.content_margin_top = 16
	style.content_margin_bottom = 16
	panel.add_theme_stylebox_override("panel", style)
	var column := VBoxContainer.new()
	column.add_theme_constant_override("separation", 12)
	panel.add_child(column)
	var header := HBoxContainer.new()
	column.add_child(header)
	var title := Label.new()
	title.text = "背包"
	title.add_theme_font_size_override("font_size", 24)
	title.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	header.add_child(title)
	refresh_button = Button.new()
	refresh_button.text = "刷新"
	refresh_button.pressed.connect(refresh)
	header.add_child(refresh_button)
	var close_button := Button.new()
	close_button.text = "关闭 [B]"
	close_button.pressed.connect(func(): panel.hide())
	header.add_child(close_button)
	status = Label.new()
	status.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	column.add_child(status)
	var scroll := ScrollContainer.new()
	scroll.size_flags_vertical = Control.SIZE_EXPAND_FILL
	scroll.horizontal_scroll_mode = ScrollContainer.SCROLL_MODE_DISABLED
	column.add_child(scroll)
	grid = GridContainer.new()
	grid.columns = 4
	grid.add_theme_constant_override("h_separation", 8)
	grid.add_theme_constant_override("v_separation", 8)
	scroll.add_child(grid)
	details = Label.new()
	details.custom_minimum_size.y = 85
	details.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	column.add_child(details)
	claim_button = Button.new()
	claim_button.text = "拾取货物凭证"
	add_child(claim_button)
	claim_button.set_anchors_and_offsets_preset(Control.PRESET_CENTER_BOTTOM)
	claim_button.offset_left = -100
	claim_button.offset_right = 100
	claim_button.offset_top = -100
	claim_button.offset_bottom = -58
	claim_button.pressed.connect(func(): claim_requested.emit())
	claim_button.hide()
	panel.hide()
	_layout()
	get_viewport().size_changed.connect(_layout)
	_render_items()
	refresh()

func _layout() -> void:
	var viewport := get_viewport().get_visible_rect().size
	panel.size = Vector2(390, minf(500, viewport.y - 24))
	panel.position = (viewport - panel.size) / 2

func is_open() -> bool:
	return is_instance_valid(panel) and panel.visible

func toggle() -> void:
	if get_parent().is_dialogue_open() or get_parent().cart_sync_busy:
		return
	panel.visible = not panel.visible
	get_parent().update_hint()

func _input(event: InputEvent) -> void:
	if is_open() and event.is_action_pressed("ui_cancel"):
		panel.hide()
		get_viewport().set_input_as_handled()

func _unhandled_key_input(event: InputEvent) -> void:
	if event is InputEventKey and event.pressed and not event.echo and event.physical_keycode == KEY_B:
		if not get_parent().is_dialogue_open():
			toggle()
			get_viewport().set_input_as_handled()

func refresh(after_claim: bool = false, success_context: String = "") -> void:
	if not success_context.is_empty():
		refresh_success_context = success_context
	elif after_claim:
		refresh_success_context = "领取成功"
	claim_refresh_pending = claim_refresh_pending or after_claim
	_cancel_request()
	refresh_button.disabled = true
	status.text = "领取成功，正在刷新背包…" if claim_refresh_pending else "正在读取背包…"
	var url := player_api_url.trim_suffix("/") + "/inventory"
	if url.begins_with("/"):
		var origin := str(JavaScriptBridge.eval("window.location.origin")) if OS.has_feature("web") else str(ProjectSettings.get_setting("quest/base_url", "http://127.0.0.1:18080"))
		url = origin.trim_suffix("/") + url
	request = HTTPRequest.new()
	request.timeout = 15
	request.body_size_limit = 262144
	add_child(request)
	request.request_completed.connect(_on_inventory.bind(generation))
	var error := request.request(url, PackedStringArray(["Accept: application/json", "Cache-Control: no-cache"]))
	if error != OK:
		_cancel_request()
		_failed("无法发起背包查询")

func _on_inventory(result: int, code: int, _headers: PackedStringArray, body: PackedByteArray, id: int) -> void:
	if id != generation:
		return
	_cancel_request()
	if result != HTTPRequest.RESULT_SUCCESS or code != 200:
		_failed("背包查询失败（HTTP %d）" % code)
		return
	var data = JSON.parse_string(body.get_string_from_utf8())
	if not data is Dictionary or not data.get("items") is Array:
		_failed("背包响应格式错误")
		return
	var seen := {}
	for item in data.items:
		if not item is Dictionary or not item.get("item_id") is String or not item.get("name") is String or not (item.get("quantity") is float or item.get("quantity") is int):
			_failed("物品数据格式错误")
			return
		if item.item_id.is_empty() or seen.has(item.item_id) or item.quantity < 0 or item.quantity != floor(item.quantity):
			_failed("物品数量或编号无效")
			return
		seen[item.item_id] = true
	items = data.items.filter(func(item): return item.quantity > 0)
	claim_refresh_pending = false
	refresh_success_context = ""
	refresh_button.disabled = false
	status.text = "背包为空" if items.is_empty() else "%d 种物品 · 点击格子查看详情" % items.size()
	_render_items()
	inventory_loaded.emit()

func _failed(reason: String) -> void:
	refresh_button.disabled = false
	var context := refresh_success_context if not refresh_success_context.is_empty() else ("领取成功" if claim_refresh_pending else "")
	status.text = (context + "，背包刷新失败" if not context.is_empty() else reason) + "，请点击刷新。"
	if not items.is_empty():
		status.text += " 当前显示上次读取的数据。"
	inventory_failed.emit(status.text)

func _render_items() -> void:
	for child in grid.get_children():
		grid.remove_child(child)
		child.queue_free()
	details.text = "选择物品查看名称和用途。"
	var group := ButtonGroup.new()
	for i in range(maxi(12, items.size())):
		var slot := Button.new()
		slot.custom_minimum_size = Vector2(76, 76)
		grid.add_child(slot)
		if i >= items.size():
			slot.disabled = true
			continue
		var item: Dictionary = items[i]
		slot.toggle_mode = true
		slot.button_group = group
		slot.tooltip_text = item.name
		var art: Dictionary = ITEM_ART.get(item.item_id, {})
		if art.has("icon"):
			var icon := TextureRect.new()
			icon.texture = art.icon
			icon.texture_filter = CanvasItem.TEXTURE_FILTER_NEAREST
			icon.expand_mode = TextureRect.EXPAND_IGNORE_SIZE
			icon.stretch_mode = TextureRect.STRETCH_KEEP_ASPECT_CENTERED
			icon.position = Vector2(14, 8)
			icon.size = Vector2(48, 48)
			icon.mouse_filter = Control.MOUSE_FILTER_IGNORE
			slot.add_child(icon)
		else:
			slot.text = "?"
		var count := Label.new()
		count.text = str(int(item.quantity))
		count.horizontal_alignment = HORIZONTAL_ALIGNMENT_RIGHT
		count.position = Vector2(4, 51)
		count.size = Vector2(65, 22)
		count.mouse_filter = Control.MOUSE_FILTER_IGNORE
		slot.add_child(count)
		slot.pressed.connect(_select_item.bind(item))
		if item.item_id == selected_id:
			slot.button_pressed = true
			_select_item(item)

func _select_item(item: Dictionary) -> void:
	selected_id = item.item_id
	details.text = "%s × %d\n%s" % [item.name, int(item.quantity), ITEM_ART.get(item.item_id, {}).get("description", "暂无物品说明。")]

func _cancel_request() -> void:
	generation += 1
	if is_instance_valid(request):
		request.cancel_request()
		request.queue_free()
		request = null

func _exit_tree() -> void:
	_cancel_request()
