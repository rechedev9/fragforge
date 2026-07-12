import assert from 'node:assert/strict';
import test from 'node:test';
import {
  PUBLISH_ASSISTANT_SCHEMA_VERSION,
  YOUTUBE_STUDIO_URL,
  isCrossSitePublishAssistantRequest,
  parsePublishAssistant,
  upcomingPublishSlots,
} from './publish-assistant.ts';

function response(): Record<string, unknown> {
  const recommendations = Array.from({ length: 3 }, (_, index) => ({
    title: `4K de Alex en Mirage ${index + 1}`,
    description: 'Cuatro bajas de Alex en Mirage. #CS2 #Shorts',
    keywords: ['Alex', 'Mirage', '4K'],
    tags: ['CS2', 'Mirage', '4K'],
    score: 90 - index,
    rationale: 'Solo usa el jugador, el mapa y el número de bajas del render.',
  }));
  return {
    schema_version: PUBLISH_ASSISTANT_SCHEMA_VERSION,
    metadata: {
      title: '4K de Alex en Mirage',
      description: 'Cuatro bajas de Alex en Mirage. #CS2 #Shorts',
      tags: ['CS2', 'Mirage', '4K'],
    },
    recommendations,
    keywords: ['Alex', 'Mirage', '4K'],
    tags: ['CS2', 'Mirage', '4K'],
    schedule: {
      time_zone: 'Europe/Madrid',
      generated_at: '2026-07-12T10:00:00Z',
      days: [
        {
          date: '2026-07-13',
          weekday: 'lunes',
          slots: [
            {
              publish_at: '2026-07-13T18:00:00Z',
              local_time: '20:00',
              source: 'baseline',
              confidence: 0.62,
              score: 0.7,
              rationale: 'Referencia determinista para Europe/Madrid.',
            },
          ],
        },
      ],
      sources: [{ title: 'YouTube Help', url: 'https://support.google.com/youtube/answer/57407?hl=es' }],
      caveat: 'Es una referencia, no una garantía de distribución.',
    },
    trends: {
      available: false,
      terms: [],
      reason: 'Firecrawl no está configurado.',
    },
    studio_url: YOUTUBE_STUDIO_URL,
  };
}

test('parses the manual publish assistant response', () => {
  const assistant = parsePublishAssistant(response());
  assert.equal(assistant.schemaVersion, PUBLISH_ASSISTANT_SCHEMA_VERSION);
  assert.equal(assistant.studioUrl, YOUTUBE_STUDIO_URL);
  assert.equal(assistant.metadata.title, '4K de Alex en Mirage');
  assert.equal(assistant.recommendations.length, 3);
  assert.deepEqual(assistant.keywords, ['Alex', 'Mirage', '4K']);
  assert.equal(assistant.schedule.timeZone, 'Europe/Madrid');
  assert.equal(assistant.schedule.days[0]?.slots[0]?.localTime, '20:00');
  assert.equal(assistant.trends.available, false);
});

test('rejects any external-open URL other than the stable YouTube Studio URL', () => {
  assert.throws(
    () => parsePublishAssistant({ ...response(), studio_url: 'https://example.test/collect' }),
    /invalid publish assistant response/,
  );
});

test('requires three to five factual recommendations', () => {
  const value = response();
  const recommendations = value.recommendations;
  assert.ok(Array.isArray(recommendations));
  assert.throws(
    () => parsePublishAssistant({ ...value, recommendations: recommendations.slice(0, 2) }),
    /invalid publish assistant response/,
  );
});

test('returns only future daily schedule slots', () => {
  const assistant = parsePublishAssistant(response());
  assert.equal(upcomingPublishSlots(assistant.schedule, Date.parse('2026-07-13T17:59:00Z')).length, 1);
  assert.equal(upcomingPublishSlots(assistant.schedule, Date.parse('2026-07-13T18:01:00Z')).length, 0);
});

test('blocks cross-site publish-assistant reads at the browser proxy boundary', () => {
  const localURL = 'http://localhost:3000/api/demos/job/renders/viral-60-clean/videos/reel/publish-assistant';
  assert.equal(isCrossSitePublishAssistantRequest(new Request(localURL, {
    headers: { 'Sec-Fetch-Site': 'cross-site', Origin: 'https://attacker.example' },
  })), true);
  assert.equal(isCrossSitePublishAssistantRequest(new Request(localURL, {
    headers: { Origin: 'https://attacker.example' },
  })), true);
  assert.equal(isCrossSitePublishAssistantRequest(new Request(localURL, {
    headers: { 'Sec-Fetch-Site': 'same-origin', Origin: 'http://localhost:3000' },
  })), false);
});
