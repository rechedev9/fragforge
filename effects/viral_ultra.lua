-- Aggressive viral preset for compiled CS2 kill shorts.
-- Runs with: --effects effects/viral_ultra.lua

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

local function upper(value)
  if value == nil then return "" end
  return string.upper(tostring(value))
end

local function weapon_label(k)
  if k.weapon == "AWP" then return "AWP PICK" end
  if k.weapon == "Hegrenade" then return "NADE KILL" end
  if k.headshot then return "HEADSHOT" end
  if nonempty(k.weapon) then return upper(k.weapon) end
  return "KILL"
end

local function milestone_label(count)
  if count == 10 then return "10 DOWN" end
  if count == 20 then return "20 DOWN" end
  if count == 30 then return "30 BOMB" end
  if count % 5 == 0 then return tostring(count) .. " KILLS" end
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
    contrast = 1.18,
    saturation = 1.28,
    gamma = 1.02
  })

  flash({
    start = 0,
    duration = 0.16,
    opacity = 0.18,
    color = "white"
  })

  text({
    value = upper(segment.player) .. " " .. tostring(segment.kill_count) .. "K",
    start = 0,
    duration = 1.55,
    x = "(w-text_w)/2",
    y = 74,
    size = 72,
    color = "#ffffff@0.98",
    box_color = "#000000@0.62",
    box_border = 24,
    fade_in = 0.06,
    fade_out = 0.18
  })

  text({
    value = "DUST2 KILL STREAK",
    start = 0.10,
    duration = 1.35,
    x = "(w-text_w)/2",
    y = 172,
    size = 34,
    color = "#f4f4f4@0.92",
    box_color = "#111111@0.46",
    box_border = 14,
    fade_in = 0.06,
    fade_out = 0.18
  })

  text({
    value = "WATCH EVERY PICK",
    start = 0.18,
    duration = 1.25,
    x = "(w-text_w)/2",
    y = 1510,
    size = 52,
    color = "#ffffff@0.96",
    box_color = "#c1121f@0.58",
    box_border = 20,
    fade_in = 0.08,
    fade_out = 0.18
  })
end)

on_kill(function(k)
  kill_number = kill_number + 1

  local label = weapon_label(k)
  local scale = 1.085
  local flash_opacity = 0.16
  local flash_duration = 0.10

  if k.weapon == "AWP" then
    scale = 1.13
    flash_opacity = 0.28
    flash_duration = 0.14
  elseif k.headshot then
    scale = 1.115
    flash_opacity = 0.23
    flash_duration = 0.12
  end

  if k.wallbang then
    scale = scale + 0.025
    label = label .. " WALLBANG"
  end

  zoom({
    at = k.time,
    pre = 0.16,
    post = 0.78,
    scale = scale
  })

  flash({
    at = k.time,
    duration = flash_duration,
    opacity = flash_opacity,
    color = "white"
  })

  text({
    value = label,
    at = k.time,
    pre = 0.05,
    post = 0.82,
    x = "(w-text_w)/2",
    y = 226,
    size = 58,
    color = "#ffffff@0.98",
    box_color = "#000000@0.62",
    box_border = 22,
    fade_in = 0.04,
    fade_out = 0.14
  })

  text({
    value = tostring(kill_number) .. "/" .. tostring(segment.kill_count),
    at = k.time,
    pre = 0.05,
    post = 0.82,
    x = "w-text_w-46",
    y = 330,
    size = 40,
    color = "#ffffff@0.96",
    box_color = "#c1121f@0.56",
    box_border = 16,
    fade_in = 0.04,
    fade_out = 0.14
  })

  if k.wallbang then
    text({
      value = "THROUGH THE WALL",
      at = k.time + 0.10,
      pre = 0,
      post = 0.86,
      x = "(w-text_w)/2",
      y = 1410,
      size = 64,
      color = "#ffffff@0.98",
      box_color = "#c1121f@0.62",
      box_border = 24,
      fade_in = 0.06,
      fade_out = 0.16
    })
  end

  if last_kill_time ~= nil and k.time - last_kill_time <= 1.35 then
    text({
      value = "FAST CHAIN",
      at = k.time,
      pre = 0.02,
      post = 0.90,
      x = "(w-text_w)/2",
      y = 1328,
      size = 62,
      color = "#ffffff@0.98",
      box_color = "#ff8c00@0.58",
      box_border = 24,
      fade_in = 0.05,
      fade_out = 0.15
    })
  end

  local milestone = milestone_label(kill_number)
  if nonempty(milestone) then
    text({
      value = milestone,
      at = k.time + 0.20,
      pre = 0,
      post = 1.05,
      x = "(w-text_w)/2",
      y = 1510,
      size = 68,
      color = "#ffffff@0.98",
      box_color = "#c1121f@0.64",
      box_border = 26,
      fade_in = 0.07,
      fade_out = 0.18
    })
  end

  last_kill_time = k.time

  if segment.kill_count >= 2 and kill_number == segment.kill_count then
    local final_start = k.time + 0.28
    if k.wallbang then
      final_start = k.time + 1.14
    end
    text({
      value = tostring(segment.kill_count) .. "K FINISH",
      start = final_start,
      duration = 1.30,
      x = "(w-text_w)/2",
      y = 1440,
      size = 74,
      color = "#ffffff@0.98",
      box_color = "#c1121f@0.66",
      box_border = 28,
      fade_in = 0.08,
      fade_out = 0.22
    })
  end
end)
