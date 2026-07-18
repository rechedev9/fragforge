import assert from 'node:assert/strict';
import test from 'node:test';
import { loadNewsDraft, saveNewsDraft, type NewsShortDraft } from './news.ts';

test('news draft round-trips through local storage', () => {
  let raw: string | null = null;
  const storage = {
    getItem: (): string | null => raw,
    setItem: (_key: string, value: string): void => { raw = value; },
  };
  const draft: NewsShortDraft = {
    sourceUrl: 'https://x.com/CounterStrike/status/1',
    channel: 'RaizerinhoCS2',
    title: 'Más skins',
    hook: '¿Y el anticheat?',
    script: 'Valve acaba de anunciar...',
    updatedAt: '2026-07-18T00:00:00.000Z',
  };
  assert.equal(saveNewsDraft(storage, draft), true);
  assert.deepEqual(loadNewsDraft(storage), draft);
});

test('news draft handles unavailable local storage', () => {
  const draft = {} as NewsShortDraft;
  assert.equal(loadNewsDraft({ getItem: () => { throw new Error('blocked'); } }), null);
  assert.equal(saveNewsDraft({ setItem: () => { throw new Error('full'); } }, draft), false);
});

test('news draft rejects malformed storage data', () => {
  assert.equal(loadNewsDraft({ getItem: () => '{bad json' }), null);
  assert.equal(loadNewsDraft({ getItem: () => JSON.stringify({ channel: 'RaizerinhoCS2' }) }), null);
});
