import test from 'node:test';
import assert from 'node:assert/strict';
import { compareHLAEVersions, parseLatestHLAERelease } from './hlae-release.ts';

const VALID_DIGEST = '2594d7cfb452ad0cec250f3c5e60c3d7209276de297efd0df2fa7ec0e8c874fa';

function release(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    tag_name: 'v2.190.2',
    assets: [
      {
        name: 'hlae_2_190_2.zip',
        browser_download_url:
          'https://github.com/advancedfx/advancedfx/releases/download/v2.190.2/hlae_2_190_2.zip',
        digest: `sha256:${VALID_DIGEST}`,
      },
    ],
    ...overrides,
  };
}

test('parses the official HLAE release asset and digest', () => {
  assert.deepEqual(parseLatestHLAERelease(release()), {
    version: '2.190.2',
    url: 'https://github.com/advancedfx/advancedfx/releases/download/v2.190.2/hlae_2_190_2.zip',
    sha256: VALID_DIGEST,
  });
});

test('rejects malformed or untrusted release metadata', () => {
  const invalidCases: unknown[] = [
    null,
    {},
    release({ tag_name: 'latest' }),
    release({ tag_name: 'v2.190' }),
    release({ assets: [] }),
    release({
      assets: [
        {
          name: 'hlae_2_190_2.zip',
          browser_download_url: 'https://example.com/hlae_2_190_2.zip',
          digest: `sha256:${VALID_DIGEST}`,
        },
      ],
    }),
    release({
      assets: [
        {
          name: 'hlae_2_190_2.zip',
          browser_download_url:
            'https://github.com/advancedfx/advancedfx/releases/download/v2.190.2/hlae_2_190_2.zip',
          digest: 'sha256:not-a-digest',
        },
      ],
    }),
  ];

  for (const value of invalidCases) assert.equal(parseLatestHLAERelease(value), null);
});

test('compares numeric HLAE versions without lexical ordering bugs', () => {
  assert.ok(compareHLAEVersions('2.190.10', '2.190.2') > 0);
  assert.ok(compareHLAEVersions('3.0.0', '2.999.999') > 0);
  assert.ok(compareHLAEVersions('2.190.1', '2.190.2') < 0);
  assert.equal(compareHLAEVersions('2.190.2', '2.190.2'), 0);
});
