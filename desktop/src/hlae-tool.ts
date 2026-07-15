// Keep this runtime constant aligned with hlae-tool.json. The unit test compares
// both representations; build scripts consume the JSON manifest directly.
export const PINNED_HLAE_TOOL = {
  version: '2.191.0',
  archiveName: 'hlae_2_191_0.zip',
  url: 'https://github.com/advancedfx/advancedfx/releases/download/v2.191.0/hlae_2_191_0.zip',
  sha256: '78efa377a2bac9522c3771a79c2503fec57e106432fc11d32244fe25b7c5b6cc',
  kind: 'zip',
  exeRel: 'HLAE.exe',
  timeoutMs: 90_000,
} as const;
