// Keep this runtime constant aligned with hlae-tool.json. The unit test compares
// both representations; build scripts consume the JSON manifest directly.
export const PINNED_HLAE_TOOL = {
  version: '2.191.1',
  archiveName: 'hlae_2_191_1.zip',
  url: 'https://github.com/advancedfx/advancedfx/releases/download/v2.191.1/hlae_2_191_1.zip',
  sha256: '307ba9170b151a7df9b7e5604b335c2d8b8df5bf5cb8d6700ae3fd01069da514',
  kind: 'zip',
  exeRel: 'HLAE.exe',
  timeoutMs: 90_000,
} as const;
