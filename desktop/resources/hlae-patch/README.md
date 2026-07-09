# FragForge HLAE Source 2 compatibility hook

This directory contains the `AfxHookSource2.dll` build that FragForge uses with
HLAE 2.190.2 after the CS2 `1.41.6.8` update. It fixes the signatures, offsets,
Panorama style resolution, and render callback slot required by HLAE capture.

The hook was built from AdvancedFX commit
`d4756a5888c4ecd1d6775a30ef78b32f58d40713` plus `SOURCE.patch`, then validated
with a real Mirage demo through the complete FragForge recorder path:

- H.264 1920x1080 at 60 fps, 360 frames / 6.0 seconds
- AAC stereo audio
- expected POV, HUD, radar, crosshair, and killfeed
- no hook crash, black frames, frozen output, or decode errors

`AfxHookSource2.dll` SHA-256:

`cc688974a3aeda59371fe399db7b91d49b5296e2afe8b15d6f9b7ebba59dc22e`

`SOURCE.patch` SHA-256:

`c58a40b5c98db09c335ed0c1a9ae9567dd3d39a6460863c2a4456476974a92f0`

Studio installs this DLL only over HLAE 2.190.2 and retains the original as
`x64/AfxHookSource2.official-2.190.2.dll`. Later HLAE versions are not patched.

AdvancedFX is licensed under the MIT License. `LICENSE`, the corresponding
source diff (`SOURCE.patch`), and the generated dependency notices
(`THIRDPARTY.yml`) are included in this directory.
