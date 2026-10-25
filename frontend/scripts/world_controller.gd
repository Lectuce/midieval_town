extends Node2D

enum Floor { OUTSIDE, FIRST, SECOND, THIRD }
const MAP_EDGE_LAYER := 128
const INTERACTION_DISTANCE := 11.0

@export var camera_zoom: float = 3.0
@export_range(8.0, 64.0) var cart_interaction_distance: float = 24.0
@export var player_api_url: String = "/api/player"
@onready var missing_cart: Node2D = $MissingCart
@onready var player: CharacterBody2D = $Player
@onready var camera: Camera2D = $Player/Camera2D
@onready var castle: Node2D = $Castle
@onready var ground: TileMapLayer = $Ground/GroundTiles
@onready var stairs_first: Marker2D = $Castle/Floor1/StairsUp
@onready var stairs_second: Marker2D = $Castle/Floor2/StairsDown
@onready var ladder_second: Marker2D = $Castle/Floor2/LadderUp
@onready var ladder_third: Marker2D = $Castle/Floor3/LadderDown
@onready var stairs: Array[Dictionary] = [
	{"from": Floor.FIRST, "to": Floor.SECOND, "entrance": stairs_first, "exit": stairs_second, "label": "上二楼"},
	{"from": Floor.SECOND, "to": Floor.FIRST, "entrance": stairs_second, "exit": stairs_first, "label": "下一楼"},
	{"from": Floor.SECOND, "to": Floor.THIRD, "entrance": ladder_second, "exit": ladder_third, "label": "上三楼"},
	{"from": Floor.THIRD, "to": Floor.SECOND, "entrance": ladder_third, "exit": ladder_second, "label": "下二楼"},
]

var current_floor: Floor = Floor.OUTSIDE
var map_rect: Rect2
var hint: Label
var dialogue
var cart_sync_busy := false
var cart_status := ""
var latest_player: Dictionary = {}
var _cart_request_id := 0
var _cart_callback: JavaScriptObject
var _cart_client: JavaScriptObject
var _player_was_processing := true
var inventory
var cart_inspected := false
var cart_claimed := false
var _cart_action := "inspect"

# Embedded so the existing Web export includes the bridge without extra files.
const CART_SYNC_JS := """
globalThis.TownCartPosition = {
 create(callback) {
  let current = null;
  function cancel() {
   const job = current;
   current = null;
   if (job) { clearTimeout(job.timer); job.controller.abort(); }
  }
  function start(id, url, snapshotJSON) {
   cancel();
   const job = {controller: new AbortController(), timer: null};
   current = job;
   let ended = false;
   function finish(event, data) {
    if (current !== job || ended) return;
    ended = true;
    callback(JSON.stringify({id, event, data}));
   }
   job.timer = setTimeout(() => {
    finish('error', {message: '调查请求超时，结果尚未确认，请按 E 重试。'});
    job.controller.abort();
   }, 15000);
   async function request(method, address, body) {
    const res = await fetch(address, {
     method, cache: 'no-store', credentials: 'same-origin',
     headers: {'Accept': 'application/json', ...(body ? {'Content-Type': 'application/json'} : {})},
     ...(body ? {body: JSON.stringify(body)} : {}), signal: job.controller.signal
    });
    if (current !== job || ended) throw new Error('cancelled');
    return res;
   }
   async function readPlayer(res) {
    if (!res.ok) throw new Error('位置请求失败（HTTP ' + res.status + '），请按 E 重试。');
    const data = await res.json();
    if (!data || !Number.isSafeInteger(data.revision) || data.revision < 1)
     throw new Error('玩家 revision 无效，请检查接口后重试。');
    return data;
   }
   void (async () => {
    try {
     const snapshot = JSON.parse(snapshotJSON);
     const player = await readPlayer(await request('GET', url));
     if (current !== job || ended) return;
     const saved = await request('PUT', (url.endsWith('/') ? url.slice(0, -1) : url) + '/position', {
      position: snapshot.position, floor: snapshot.floor, expected_revision: player.revision
     });
     if (saved.status === 409) {
      const refreshed = await readPlayer(await request('GET', url));
      finish('conflict', refreshed);
      return; // Refresh only; never automatically retry PUT or award evidence.
     }
	const result = await readPlayer(saved);

	if (!result.position || result.position.x !== snapshot.position.x ||
		result.position.y !== snapshot.position.y || result.floor !== snapshot.floor ||
		result.revision < player.revision) {
	throw new Error('服务器位置确认不一致，请按 E 重新同步。');
	}

	// 位置确认成功后，再申请调查。
	// 使用与玩家接口相同的服务器地址。
	const playerURL = new URL(url, window.location.href);
	if (snapshot.action === 'claim') {
	 const claimed = await request('POST', new URL('/api/investigation-points/abandoned_cart/claim', playerURL).href);
	 const claim = await claimed.json();
	 if (!claimed.ok) throw new Error(typeof claim?.error === 'string' ? claim.error : '领取失败，请重试。');
	 if (claim.source_id !== 'abandoned_cart' || claim.item_id !== 'shipment_receipt' ||
	     typeof claim.newly_claimed !== 'boolean' || !Number.isSafeInteger(claim.quantity) || claim.quantity < 0 ||
	     typeof claim.message !== 'string' || !claim.message.trim()) throw new Error('领取响应不完整，请重试确认。');
	 finish('claimed', {player: result, claim});
	 return;
	}
	const inspectURL = new URL(
	'/api/investigation-points/abandoned_cart/inspect',
	playerURL
	).href;

	const inspected = await request('POST', inspectURL);

	// 普通 JSON 响应，包括后端返回的业务错误。
	let investigation;
	try {
	investigation = await inspected.json();
	} catch {
	throw new Error('调查接口返回格式错误，请按 E 重试。');
	}

	if (!inspected.ok) {
	throw new Error(
	typeof investigation?.error === 'string'
	? investigation.error
	: '调查失败（HTTP ' + inspected.status + '），请按 E 重试。'
	);
	}

	// 确认收到的是本次货车调查的有效结果。
	if (!investigation ||
		investigation.point_id !== 'abandoned_cart' ||
		investigation.evidence_id !== 'cart_found' ||
		typeof investigation.newly_found !== 'boolean' ||
		typeof investigation.message !== 'string' ||
		investigation.message.trim() === '') {
	throw new Error('调查结果不完整，请按 E 重试。');
	}

	// 只有调查接口成功后，才通知 Godot 本次操作完成。
	finish('done', {
	player: result,
	investigation: investigation
	});
    } catch (error) {
     finish('error', {message: typeof error?.message === 'string' ? error.message : '调查请求失败，请按 E 重试。'});
    } finally {
     clearTimeout(job.timer);
     job.controller.abort();
     if (current === job) current = null;
    }
   })();
  }
  return {start, cancel};
 }
};
"""

func _ready() -> void:
	texture_filter = CanvasItem.TEXTURE_FILTER_NEAREST
	configure_map_bounds()
	configure_camera()
	create_hint()
	dialogue = preload("res://scripts/dialogue_box.gd").new()
	dialogue.name = "DialogueBox"
	add_child(dialogue)
	inventory = preload("res://scripts/inventory_panel.gd").new()
	inventory.name = "Inventory"
	inventory.player_api_url = player_api_url
	add_child(inventory)
	inventory.claim_requested.connect(func(): sync_cart_position(true))
	set_floor(Floor.OUTSIDE)
	get_viewport().size_changed.connect(update_camera_zoom)

func configure_map_bounds() -> void:
	var cells := ground.get_used_rect()
	var tile_size := Vector2(ground.tile_set.tile_size)
	map_rect = Rect2(ground.to_global(Vector2(cells.position) * tile_size), Vector2(cells.size) * tile_size)
	var edges := StaticBody2D.new()
	edges.name = "MapBounds"
	edges.collision_layer = MAP_EDGE_LAYER
	edges.collision_mask = 0
	add_child(edges)
	# Place the inner faces at the painted ground's four edges.
	var p := map_rect.position
	var s := map_rect.size
	add_boundary(edges, "Left", Rect2(p + Vector2(-32, -32), Vector2(32, s.y + 64)))
	add_boundary(edges, "Right", Rect2(p + Vector2(s.x, -32), Vector2(32, s.y + 64)))
	add_boundary(edges, "Top", Rect2(p + Vector2(0, -32), Vector2(s.x, 32)))
	add_boundary(edges, "Bottom", Rect2(p + Vector2(0, s.y), Vector2(s.x, 32)))

func add_boundary(body: StaticBody2D, node_name: String, rect: Rect2) -> void:
	var collider := CollisionShape2D.new()
	collider.name = node_name
	var shape := RectangleShape2D.new()
	shape.size = rect.size
	collider.shape = shape
	body.add_child(collider)
	collider.global_position = rect.get_center()

func configure_camera() -> void:
	$Camera2D.enabled = false
	camera.enabled = true
	camera.position = Vector2.ZERO
	camera.offset = Vector2.ZERO
	camera.position_smoothing_enabled = false
	camera.limit_smoothed = false
	camera.limit_left = int(map_rect.position.x)
	camera.limit_top = int(map_rect.position.y)
	camera.limit_right = int(map_rect.end.x)
	camera.limit_bottom = int(map_rect.end.y)
	update_camera_zoom()
	camera.make_current()
	camera.force_update_scroll()

func update_camera_zoom() -> void:
	# A larger window must not reveal space beyond the map limits.
	var viewport_size := get_viewport_rect().size
	var fit_zoom := maxf(viewport_size.x / map_rect.size.x, viewport_size.y / map_rect.size.y)
	var value := maxf(camera_zoom, fit_zoom)
	camera.zoom = Vector2.ONE * value
	camera.force_update_scroll()

func after_player_move(previous_position: Vector2) -> void:
	if is_instance_valid(inventory) and inventory.is_open():
		update_hint()
		return
	if cart_sync_busy:
		update_hint()
		return
	if is_dialogue_open():
		if not is_instance_valid(dialogue.speaker):
			dialogue.close()
		update_hint()
		return
	var previous := castle.to_local(previous_position)
	var current := castle.to_local(player.global_position)
	if current_floor == Floor.OUTSIDE:
		# Only crossing the front threshold inward grants access to the ladder.
		if previous.y >= 0.0 and current.y < 0.0 and absf(current.x) <= 7.1:
			set_floor(Floor.FIRST)
	elif current_floor == Floor.FIRST:
		if previous.y < 0.0 and current.y >= 0.0 and absf(current.x) <= 7.1:
			set_floor(Floor.OUTSIDE)
	if Input.is_action_just_pressed("interact"):
		var npc := dialogue_target()
		if npc != null:
			dialogue.open(npc)
			player.velocity = Vector2.ZERO
		elif can_investigate_cart():
			sync_cart_position()
		else:
			try_use_ladder()
	update_hint()

func is_dialogue_open() -> bool:
	return is_instance_valid(dialogue) and dialogue.active

func is_ui_blocked() -> bool:
	return is_dialogue_open() or (is_instance_valid(inventory) and inventory.is_open()) or cart_sync_busy

func dialogue_target() -> StaticBody2D:
	var closest := INF
	var result: StaticBody2D
	var stair := nearby_stair()
	# Compare distances so the ladder remains usable beside the upstairs guard.
	if not stair.is_empty():
		closest = player.global_position.distance_to(stair["entrance"].global_position)
	for npc in get_tree().get_nodes_in_group("talkable_npcs"):
		if not npc.is_visible_in_tree() or (npc.collision_layer & (1 << int(current_floor))) == 0:
			continue
		var distance := player.global_position.distance_to(npc.global_position)
		if distance > npc.talk_distance or distance >= closest:
			continue
		var ray := PhysicsRayQueryParameters2D.create(player.global_position, npc.global_position + Vector2(0, -1), 1 << int(current_floor), [player.get_rid()])
		var hit := get_world_2d().direct_space_state.intersect_ray(ray)
		if not hit.is_empty() and hit["collider"] != npc:
			continue
		closest = distance
		result = npc
	return result

func try_use_ladder() -> void:
	var target := nearby_stair()
	if target.is_empty():
		return
	set_floor(target["to"])
	player.global_position = target["exit"].global_position
	player.velocity = Vector2.ZERO
	update_hint()

func nearby_stair() -> Dictionary:
	var result: Dictionary = {}
	var closest := INTERACTION_DISTANCE
	for stair in stairs:
		if stair["from"] != current_floor:
			continue
		var distance := player.global_position.distance_to(stair["entrance"].global_position)
		if distance <= closest:
			closest = distance
			result = stair
	return result

func set_floor(floor_value: Floor) -> void:
	current_floor = floor_value
	var floor_layer := 1 << int(current_floor)
	player.collision_layer = floor_layer
	player.collision_mask = floor_layer | MAP_EDGE_LAYER
	player.z_index = 1 if current_floor == Floor.OUTSIDE else (2 if current_floor == Floor.THIRD else 3)
	# Reveal the occupied floor beneath the facade; the tower caps remain visible.
	var first := current_floor == Floor.FIRST
	var second := current_floor == Floor.SECOND
	$Castle/Floor1/Interior.visible = first
	$Castle/Floor2/Interior.visible = second
	# The indoor cutaway must not leave fragments of the outside guards visible.
	$Guard.visible = not first
	$Guard2.visible = not first
	# Upper-floor occupants must not show through the interior cutaway.
	# Visibility does not disable their separate third-floor collision layer.
	$Castle/Floor3.visible = not (first or second)
	$Castle/Visuals/UpperPlatform.modulate.a = 0.2 if first or second else 1.0
	$Castle/Visuals/UpperFacade.modulate.a = 0.2 if first or second else 1.0
	$Castle/Visuals/GateFacade.modulate.a = 0.2 if first else 1.0
	$Castle/Visuals/TowerMiddle.modulate.a = 0.2 if first or second else 1.0
	$Castle/Visuals/LowPlatforms.modulate.a = 0.2 if first else 1.0
	$Castle/Visuals/TowerBase.modulate.a = 0.2 if first else 1.0
	update_hint()

func create_hint() -> void:
	var hud := CanvasLayer.new()
	hud.name = "FloorHUD"
	add_child(hud)
	hint = Label.new()
	hint.name = "Hint"
	hud.add_child(hint)
	hint.set_anchors_and_offsets_preset(Control.PRESET_BOTTOM_WIDE)
	hint.offset_top = -44
	hint.offset_bottom = -12
	hint.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	hint.mouse_filter = Control.MOUSE_FILTER_IGNORE
	hint.add_theme_font_size_override("font_size", 20)
	hint.add_theme_color_override("font_outline_color", Color.BLACK)
	hint.add_theme_constant_override("outline_size", 5)

func update_hint() -> void:
	if not is_instance_valid(hint):
		return
	if is_instance_valid(inventory):
		inventory.claim_button.visible = cart_inspected and can_investigate_cart() and not is_dialogue_open() and not inventory.is_open()
		inventory.claim_button.disabled = cart_sync_busy or cart_claimed
		inventory.claim_button.text = "已领取货物凭证" if cart_claimed else ("领取中…" if cart_sync_busy and _cart_action == "claim" else "拾取货物凭证")
	hint.visible = not is_dialogue_open()
	if is_dialogue_open():
		return
	if cart_sync_busy:
		hint.text = "正在领取货物凭证…" if _cart_action == "claim" else "正在调查货车…"
		return
	var npc := dialogue_target()
	if npc != null:
		hint.text = "E：与%s对话" % npc.display_name
		return
	var target := nearby_stair()
	if not target.is_empty():
		hint.text = "%d 楼 · E：%s" % [int(current_floor), target["label"]]
		return
	if can_investigate_cart():
		hint.text = "E：调查货车" if cart_status.is_empty() else cart_status + "    E：调查货车"
		return
	cart_status = ""
	match current_floor:
		Floor.OUTSIDE:
			var local := castle.to_local(player.global_position)
			hint.text = "向上走，从正门进入城堡" if absf(local.x) < 22 and local.y >= 0 and local.y < 32 else "方向键移动"
		Floor.FIRST:
			hint.text = "一楼大厅：左右两侧可通行；中央偏左楼梯上二楼，正门出城堡"
		Floor.SECOND:
			hint.text = "二楼：左右通往低平台；左侧楼梯下一楼，中央梯子上三楼"
		Floor.THIRD:
			hint.text = "三楼高台：回到梯子口，按 E 返回二楼"

func can_investigate_cart() -> bool:
	return current_floor == Floor.OUTSIDE and is_instance_valid(missing_cart) \
		and missing_cart.is_visible_in_tree() \
		and player.global_position.distance_to(missing_cart.global_position) <= cart_interaction_distance

func sync_cart_position(claim: bool = false) -> void:
	if cart_sync_busy or is_ui_blocked() or not can_investigate_cart():
		return
	if claim and (not cart_inspected or cart_claimed):
		return
	if not OS.has_feature("web"):
		cart_status = "请在 Web 版本中测试货车调查"
		update_hint()
		return
	if _cart_client == null:
		JavaScriptBridge.eval(CART_SYNC_JS, true)
		_cart_callback = JavaScriptBridge.create_callback(_on_cart_response)
		_cart_client = JavaScriptBridge.get_interface("TownCartPosition").create(_cart_callback)
	_cart_request_id += 1
	_cart_action = "claim" if claim else "inspect"
	cart_sync_busy = true
	cart_status = ""
	_player_was_processing = player.is_physics_processing()
	player.velocity = Vector2.ZERO
	player.set_physics_process(false)
	var position_in_town := to_local(player.global_position)
	update_hint()
	_cart_client.start(_cart_request_id, player_api_url, JSON.stringify({
		"position": {"x": position_in_town.x, "y": position_in_town.y},
		"floor": int(current_floor),
		"action": _cart_action,
	}))

func _on_cart_response(arguments: Array) -> void:
	if arguments.size() == 1:
		_finish_cart_response.call_deferred(str(arguments[0]))

func _finish_cart_response(payload: String) -> void:
	var response = JSON.parse_string(payload)
	if not response is Dictionary or not cart_sync_busy or int(response.get("id", -1)) != _cart_request_id:
		return
	var data: Dictionary = response.get("data", {})
	match response.get("event", ""):
		"claimed":
			latest_player = data.player
			cart_claimed = true
			cart_status = str(data.claim.message)
			inventory.refresh(true)
			inventory.panel.show()
		"done":
			var saved_player = data.get("player")
			var investigation = data.get("investigation")

			if saved_player is Dictionary and investigation is Dictionary:
				latest_player = saved_player
				cart_inspected = true
				cart_status = str(investigation.get("message", "调查结果缺少提示信息。"))
			else:
				cart_status = "调查响应格式错误，请按 E 重试。"
		"conflict":
			latest_player = data
			cart_status = "位置版本冲突，已重新查询；请重试"
		_:
			cart_status = str(data.get("message", "调查请求失败，请按 E 重试"))
	_cart_request_id += 1
	cart_sync_busy = false
	player.set_physics_process(_player_was_processing)
	update_hint()

func _exit_tree() -> void:
	_cart_request_id += 1
	if _cart_client != null:
		_cart_client.cancel()
