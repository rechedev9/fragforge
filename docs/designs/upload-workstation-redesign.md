# Upload workstation redesign

## Goal

The no-login upload screen should feel like the first operational step of FragForge Studio rather than a narrow marketing landing page.
The demo drop target remains the dominant action, while the surrounding chrome communicates that processing is private, local, and part of a three-stage workflow.

## Approved direction

Use the existing blue-black canvas, cyan primary signal, angular display type, and restrained grid texture.
Expand the standalone workspace to the same generous width as the Studio shell and give the top bar a centered current-stage marker.
Keep the page header concise and left-aligned.
Make the drop target wider and shallower, with its privacy facts grouped into a dedicated lower rail.
Present the three workflow steps as connected operational cards with distinct icons.
Use the footer for persistent local-processing and supported-format facts instead of a slogan.

## Interaction and accessibility

The complete drop surface remains a native label for the hidden file input.
Drag, focus, validation, offline, scanning, player-selection, and parsing behavior remain unchanged.
Controls retain visible focus rings and a minimum 40-pixel target.
On small screens, the centered stage marker disappears, cards stack, and footer facts wrap without horizontal overflow.
Reduced-motion and forced-colors behavior continue to come from the shared design system.

## Non-goals

This change does not alter upload APIs, accepted file limits, roster aggregation, parsing, navigation, or Electron IPC.
It does not introduce fabricated progress or media metadata.
