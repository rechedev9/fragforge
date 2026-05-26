-- Ma1wel AWP edit test.
-- Runs in zv-editor's Lua sandbox. The source POV clips stay untouched.

local kill_index = 0
local total_kills = 0

local function nonempty(value)
  return value ~= nil and tostring(value) ~= ""
end

local function upper(value)
  if value == nil then return "" end
  return string.upper(tostring(value))
end

on_segment(function(s)
  kill_index = 0
  total_kills = s.kill_count or 0

  grade({
    contrast = 1.10,
    saturation = 1.08,
    gamma = 1.01
  })

  text({
    value = "MA1WEL AWP",
    start = 0,
    duration = 2.0,
    x = "(w-text_w)/2",
    y = 86,
    size = 54,
    color = "white@0.96",
    box_color = "black@0.44",
    box_border = 20
  })

  local round = ""
  if s.round ~= nil then round = "ROUND " .. tostring(s.round) end
  local subtitle = round
  if total_kills > 1 then
    subtitle = subtitle .. "  |  " .. tostring(total_kills) .. "K"
  end

  if nonempty(subtitle) then
    text({
      value = subtitle,
      start = 0,
      duration = 2.0,
      x = "(w-text_w)/2",
      y = 166,
      size = 30,
      color = "white@0.90",
      box_color = "black@0.34",
      box_border = 14
    })
  end
end)

on_kill(function(k)
  kill_index = kill_index + 1

  zoom({
    at = k.time,
    pre = 0.34,
    post = 0.92,
    scale = 1.035
  })

  flash({
    at = k.time,
    duration = 0.05,
    opacity = 0.08,
    color = "white"
  })

  local label = "AWP PICK"
  if total_kills > 1 then
    label = tostring(kill_index) .. "/" .. tostring(total_kills) .. " AWP"
  end
  if k.headshot then label = label .. " HS" end
  if k.wallbang then label = label .. " WALLBANG" end

  text({
    value = label,
    at = k.time,
    pre = 0.10,
    post = 0.95,
    x = 48,
    y = 250,
    size = 38,
    color = "white@0.96",
    box_color = "black@0.42",
    box_border = 16
  })

  if nonempty(k.victim) then
    text({
      value = "vs " .. upper(k.victim),
      at = k.time,
      pre = 0.08,
      post = 0.80,
      x = 48,
      y = 318,
      size = 28,
      color = "white@0.88",
      box_color = "black@0.30",
      box_border = 12
    })
  end

  if k.wallbang then
    text({
      value = "WALLBANG",
      at = k.time + 0.14,
      pre = 0.0,
      post = 0.92,
      x = "(w-text_w)/2",
      y = 1420,
      size = 58,
      color = "white@0.98",
      box_color = "black@0.52",
      box_border = 22
    })
  end
end)
