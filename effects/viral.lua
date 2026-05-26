-- Aggressive local preset for vertical CS2 shorts.
-- Runs with: --effects effects/viral.lua

local segment = {
  player = "",
  map = "",
  kill_count = 0,
  primary_weapon = "",
  duration = 0
}

local kill_number = 0
local last_kill_time = nil

local function nonempty(value)
  return value ~= nil and tostring(value) ~= ""
end

local function kill_title(count, weapon)
  local title = "CS2 HIGHLIGHT"
  if count ~= nil and count > 0 then
    title = tostring(count) .. "K"
  end
  if nonempty(weapon) then
    title = title .. " " .. weapon
  end
  return title
end

local function join2(left, right)
  if nonempty(left) and nonempty(right) then
    return tostring(left) .. " | " .. tostring(right)
  end
  if nonempty(left) then return tostring(left) end
  if nonempty(right) then return tostring(right) end
  return ""
end

on_segment(function(s)
  segment.player = s.player or ""
  segment.map = s.map or ""
  segment.kill_count = s.kill_count or 0
  segment.primary_weapon = s.primary_weapon or ""
  segment.duration = s.duration or 0
  kill_number = 0
  last_kill_time = nil

  grade({
    contrast = 1.14,
    saturation = 1.20,
    gamma = 1.02
  })

  text({
    value = kill_title(segment.kill_count, segment.primary_weapon),
    start = 0,
    duration = 2.2,
    x = "(w-text_w)/2",
    y = 88,
    size = 58,
    color = "white@0.98",
    box_color = "black@0.56",
    box_border = 22
  })

  local tag = join2(segment.player, segment.map)
  if nonempty(tag) then
    text({
      value = string.upper(tag),
      start = 0,
      duration = 2.2,
      x = "(w-text_w)/2",
      y = 172,
      size = 30,
      color = "white@0.92",
      box_color = "black@0.42",
      box_border = 14
    })
  end

  local subtitle = join2(segment.map, segment.primary_weapon)
  if nonempty(subtitle) then
    text({
      value = subtitle,
      start = 0,
      duration = 2.2,
      x = "(w-text_w)/2",
      y = 224,
      size = 32,
      color = "white@0.94",
      box_color = "black@0.38",
      box_border = 14
    })
  end

  text({
    value = "CS2 SHORTS",
    start = 0,
    duration = 2.2,
    x = "(w-text_w)/2",
    y = 1588,
    size = 46,
    color = "white@0.94",
    box_color = "black@0.44",
    box_border = 18
  })
end)

on_kill(function(k)
  kill_number = kill_number + 1

  local label = k.weapon
  local scale = 1.055

  if k.weapon == "AWP" then
    label = "AWP PICK"
    scale = 1.10
    flash({
      at = k.time,
      duration = 0.18,
      opacity = 0.22,
      color = "white"
    })
  elseif k.headshot then
    label = "HEADSHOT"
    scale = 1.075
  elseif not nonempty(label) then
    label = "KILL"
  end

  if k.wallbang then
    label = label .. " WALLBANG"
  end

  zoom({
    at = k.time,
    pre = 0.20,
    post = 0.86,
    scale = scale
  })

  text({
    value = label,
    at = k.time,
    pre = 0.08,
    post = 1.0,
    x = 48,
    y = 250,
    size = 42,
    color = "white@0.98",
    box_color = "black@0.52",
    box_border = 18
  })

  if last_kill_time ~= nil and k.time - last_kill_time <= 1.25 then
    text({
      value = "FAST DOUBLE",
      at = k.time,
      pre = 0.05,
      post = 1.15,
      x = "(w-text_w)/2",
      y = 1420,
      size = 64,
      color = "white@0.98",
      box_color = "black@0.60",
      box_border = 24
    })
  end
  last_kill_time = k.time

  if segment.kill_count >= 2 and kill_number == segment.kill_count then
    text({
      value = tostring(segment.kill_count) .. "K COMPLETE",
      start = k.time + 0.25,
      duration = 1.2,
      x = "(w-text_w)/2",
      y = 1420,
      size = 64,
      color = "white@0.98",
      box_color = "black@0.60",
      box_border = 24
    })
  end
end)
