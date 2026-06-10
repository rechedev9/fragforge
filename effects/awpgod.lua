-- Local FragForge effects preset for fast visual iteration.
-- Runs inside zv-editor, not HLAE.

on_segment(function(s)
  if s.preset == "short-clean" then
    text({
      value = s.label,
      start = 0,
      duration = 2.4,
      x = 48,
      y = 176,
      size = 34,
      color = "white@0.94",
      box_color = "black@0.42",
      box_border = 14
    })

    local footer = "CS2 HIGHLIGHT"
    if s.kill_count > 0 and s.primary_weapon ~= "" then
      footer = tostring(s.kill_count) .. "K " .. s.primary_weapon
    elseif s.primary_weapon ~= "" then
      footer = s.primary_weapon
    end

    text({
      value = footer,
      start = 0,
      duration = 2.4,
      x = "(w-text_w)/2",
      y = 1588,
      size = 52,
      color = "white@0.96",
      box_color = "black@0.44",
      box_border = 20
    })
  end

  grade({
    contrast = 1.08,
    saturation = 1.12,
    gamma = 1.02
  })
end)

on_kill(function(k)
  local scale = 1.045
  local post = 0.68

  if k.weapon == "AWP" then
    scale = 1.085
    post = 0.84
    flash({
      at = k.time,
      duration = 0.075,
      opacity = 0.16,
      color = "white"
    })
  elseif k.headshot then
    scale = 1.065
  end

  zoom({
    at = k.time,
    pre = 0.24,
    post = post,
    scale = scale
  })

  if k.preset == "short-clean" and k.weapon ~= "" then
    local label = k.weapon
    if k.headshot then label = label .. " HS" end
    if k.wallbang then label = label .. " WB" end

    text({
      value = label,
      at = k.time,
      pre = 0.12,
      post = 0.95,
      x = 48,
      y = 254,
      size = 30,
      color = "white@0.92",
      box_color = "black@0.32",
      box_border = 14
    })
  end
end)
