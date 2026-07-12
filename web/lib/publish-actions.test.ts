import assert from 'node:assert/strict';
import test from 'node:test';
import { YOUTUBE_STUDIO_URL, parsePublishAssistant } from './api/publish-assistant.ts';
import {
  copyPublishText,
  downloadPublishMP4,
  initialPublishDraft,
  openYouTubeStudio,
  publishTagsText,
  recommendedPublishDraft,
} from './publish-actions.ts';

function assistant() {
  const recommendation = {
    title: '4K de Alex en Mirage',
    description: 'Cuatro bajas de Alex en Mirage.',
    keywords: ['Alex', 'Mirage', '4K'],
    tags: ['CS2', 'Mirage', '4K'],
    score: 92,
    rationale: 'Solo usa hechos del render.',
  };
  return parsePublishAssistant({
    schema_version: '1.0',
    metadata: { title: 'Alex en Mirage', description: 'POV completo.', tags: ['CS2', 'Mirage'] },
    recommendations: [
      recommendation,
      { ...recommendation, title: 'Mirage 4K de Alex' },
      { ...recommendation, title: 'POV: Alex consigue 4 bajas' },
    ],
    keywords: ['Alex', 'Mirage', '4K'],
    tags: ['CS2', 'Mirage', '4K'],
    schedule: {
      time_zone: 'Europe/Madrid',
      generated_at: '2026-07-12T10:00:00Z',
      days: [{
        date: '2026-07-13',
        weekday: 'lunes',
        slots: [{
          publish_at: '2026-07-13T18:00:00Z',
          local_time: '20:00',
          source: 'baseline',
          confidence: 0.62,
          score: 0.7,
          rationale: 'Referencia general.',
        }],
      }],
      sources: [],
      caveat: 'Referencia orientativa.',
    },
    trends: { available: false, terms: [], sources: [], reason: 'Sin Firecrawl.' },
    studio_url: YOUTUBE_STUDIO_URL,
  });
}

test('selecting a recommendation replaces editable title, description, and tags', () => {
  const value = assistant();
  assert.deepEqual(initialPublishDraft(value), {
    title: 'Alex en Mirage',
    description: 'POV completo.',
    tags: ['CS2', 'Mirage'],
  });
  assert.deepEqual(recommendedPublishDraft(value.recommendations[0]), {
    title: '4K de Alex en Mirage',
    description: 'Cuatro bajas de Alex en Mirage.',
    tags: ['CS2', 'Mirage', '4K'],
  });
});

test('copies title, description, and formatted tags through the clipboard', async () => {
  const copied: string[] = [];
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'navigator');
  Object.defineProperty(globalThis, 'navigator', {
    configurable: true,
    value: { clipboard: { writeText: async (value: string) => { copied.push(value); } } },
  });
  try {
    await copyPublishText('Título');
    await copyPublishText('Descripción');
    await copyPublishText(publishTagsText(['CS2', 'Mirage', '4K']));
  } finally {
    if (previous) Object.defineProperty(globalThis, 'navigator', previous);
    else Reflect.deleteProperty(globalThis, 'navigator');
  }
  assert.deepEqual(copied, ['Título', 'Descripción', 'CS2, Mirage, 4K']);
});

test('downloads the MP4 with a safe filename and clicks the temporary anchor', () => {
  const events: string[] = [];
  const anchor = {
    href: '',
    download: '',
    rel: '',
    click: () => events.push('click'),
    remove: () => events.push('remove'),
  };
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'document');
  Object.defineProperty(globalThis, 'document', {
    configurable: true,
    value: {
      createElement: (tag: string) => {
        assert.equal(tag, 'a');
        return anchor;
      },
      body: { appendChild: (value: unknown) => { assert.equal(value, anchor); events.push('append'); } },
    },
  });
  try {
    downloadPublishMP4('/api/reel.mp4', 'Mirage: 4K');
  } finally {
    if (previous) Object.defineProperty(globalThis, 'document', previous);
    else Reflect.deleteProperty(globalThis, 'document');
  }
  assert.equal(anchor.href, '/api/reel.mp4');
  assert.equal(anchor.download, 'Mirage- 4K.mp4');
  assert.equal(anchor.rel, 'noopener');
  assert.deepEqual(events, ['append', 'click', 'remove']);
});

test('opens only the stable YouTube Studio URL', () => {
  const calls: unknown[][] = [];
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'window');
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    value: { open: (...args: unknown[]) => { calls.push(args); } },
  });
  try {
    openYouTubeStudio();
  } finally {
    if (previous) Object.defineProperty(globalThis, 'window', previous);
    else Reflect.deleteProperty(globalThis, 'window');
  }
  assert.deepEqual(calls, [[YOUTUBE_STUDIO_URL, '_blank', 'noopener,noreferrer']]);
});
