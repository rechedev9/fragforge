-- Clean premium viral preset for vertical CS2 shorts.
-- Runs with: --effects effects/viral_premium.lua

local segment = {
  player = "",
  map = "",
  kill_count = 0,
  primary_weapon = ""
}

local kill_number = 0
local last_kill_time = nil
local combo_active = false

local function nonempty(value)
  return value ~= nil and tostring(value) ~= ""
end

local function join2(left, right)
  if nonempty(left) and nonempty(right) then
    return tostring(left) .. " | " .. tostring(right)
  end
  if nonempty(left) then return tostring(left) end
  if nonempty(right) then return tostring(right) end
  return ""
end

local function headline(count, weapon)
  local value = "CS2 HIGHLIGHT"
  if count ~= nil and count > 0 then
    value = tostring(count) .. "K"
  end
  if nonempty(weapon) then
    value = value .. " " .. weapon
  end
  return value
end

local function combo_label(count)
  if count == 2 then return "FAST DOUBLE" end
  if count == 3 then return "TRIPLE" end
  return "CHAIN"
end

local function final_label(count)
  if count == 5 then return "ACE" end
  if count >= 3 then return tostring(count) .. "K ROUND" end
  return tostring(count) .. "K COMPLETE"
end

on_segment(function(s)
  segment.player = s.player or ""
  segment.map = s.map or ""
  segment.kill_count = s.kill_count or 0
  segment.primary_weapon = s.primary_weapon or ""
  kill_number = 0
  last_kill_time = nil
  combo_active = false

  grade({
    contrast = 1.06,
    saturation = 1.10,
    gamma = 1.00
  })

  text({
    value = headline(segment.kill_count, segment.primary_weapon),
    start = 0,
    duration = 2.0,
    x = "(w-text_w)/2",
    y = 88,
    size = 52,
    color = "white@0.96",
    box_color = "black@0.46",
    box_border = 18,
    fade_in = 0.16,
    fade_out = 0.24
  })

  local tag = join2(segment.player, segment.map)
  if nonempty(tag) then
    text({
      value = string.upper(tag),
      start = 0,
      duration = 2.0,
      x = "(w-text_w)/2",
      y = 160,
      size = 28,
      color = "white@0.90",
      box_color = "black@0.34",
      box_border = 12,
      fade_in = 0.18,
      fade_out = 0.24
    })
  end

  text({
    value = "CS2 SHORTS",
    start = 0,
    duration = 2.0,
    x = "(w-text_w)/2",
    y = 1600,
    size = 38,
    color = "white@0.90",
    box_color = "black@0.34",
    box_border = 14,
    fade_in = 0.18,
    fade_out = 0.24
  })
end)

on_kill(function(k)
  kill_number = kill_number + 1

  local label = k.weapon
  local scale = 1.040

  if k.weapon == "AWP" then
    label = "AWP PICK"
    scale = 1.075
    flash({
      at = k.time,
      duration = 0.10,
      opacity = 0.14,
      color = "white"
    })
  elseif k.headshot then
    label = "HEADSHOT"
    scale = 1.055
  elseif not nonempty(label) then
    label = "KILL"
  end

  if k.wallbang then
    label = label .. " WALLBANG"
  end

  zoom({
    at = k.time,
    pre = 0.18,
    post = 0.72,
    scale = scale
  })

  text({
    value = label,
    at = k.time,
    pre = 0.08,
    post = 0.90,
    x = 48,
    y = 250,
    size = 36,
    color = "white@0.96",
    box_color = "black@0.44",
    box_border = 16,
    fade_in = 0.06,
    fade_out = 0.16
  })

  if segment.kill_count > 1 then
    text({
      value = tostring(kill_number) .. "/" .. tostring(segment.kill_count),
      at = k.time,
      pre = 0.08,
      post = 0.90,
      x = "w-text_w-48",
      y = 250,
      size = 32,
      color = "white@0.92",
      box_color = "black@0.36",
      box_border = 14,
      fade_in = 0.06,
      fade_out = 0.16
    })
  end

  combo_active = false
  if last_kill_time ~= nil and k.time - last_kill_time <= 1.25 then
    combo_active = true
    text({
      value = combo_label(kill_number),
      at = k.time,
      pre = 0.05,
      post = 1.05,
      x = "(w-text_w)/2",
      y = 1420,
      size = 56,
      color = "white@0.96",
      box_color = "black@0.48",
      box_border = 20,
      fade_in = 0.10,
      fade_out = 0.20
    })
  end
  last_kill_time = k.time

  if segment.kill_count >= 2 and kill_number == segment.kill_count then
    local start_time = k.time + 0.25
    if combo_active then
      start_time = k.time + 1.10
    end
    text({
      value = final_label(segment.kill_count),
      start = start_time,
      duration = 1.15,
      x = "(w-text_w)/2",
      y = 1420,
      size = 56,
      color = "white@0.96",
      box_color = "black@0.48",
      box_border = 20,
      fade_in = 0.12,
      fade_out = 0.24
    })
  end
end)
