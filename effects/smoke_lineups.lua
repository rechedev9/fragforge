-- Natural FragForge preset for smoke lineup clips.
-- Runs inside zv-editor, not HLAE.

on_segment(function(s)
  if s.smoke_count and s.smoke_count > 0 then
    text({
      value = "SMOKE LINEUP",
      start = 0,
      duration = 2.2,
      x = "(w-text_w)/2",
      y = 92,
      size = 48,
      color = "white@0.94",
      box_color = "black@0.38",
      box_border = 18
    })
  end
end)

on_smoke(function(smoke)
  local route = smoke.destination
  if route == "" then route = "UNMATCHED SMOKE" end
  if smoke.from_area ~= "" and smoke.destination ~= "" then
    route = smoke.from_area .. " -> " .. smoke.destination
  end

  text({
    value = route,
    at = smoke.time,
    pre = 0.05,
    post = 2.15,
    x = "(w-text_w)/2",
    y = 142,
    size = 42,
    color = "white@0.94",
    box_color = "black@0.36",
    box_border = 16
  })

  if smoke.destination ~= "" and smoke.pop_time and smoke.pop_time > 0 then
    text({
      value = "LANDS: " .. smoke.destination,
      at = smoke.pop_time,
      pre = 0.15,
      post = 1.65,
      x = "(w-text_w)/2",
      y = 1540,
      size = 46,
      color = "white@0.94",
      box_color = "black@0.40",
      box_border = 18
    })
  end
end)
