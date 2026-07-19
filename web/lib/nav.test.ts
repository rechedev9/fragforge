// Unit tests for the shared Studio nav data: the sidebar and every page
// header derive their numbering from NAV_SECTIONS, so the invariants below
// (zero-padded, sequential, unique hrefs) keep them from drifting again.
// Run: node --test "lib/**/*.test.ts"
import test from 'node:test';
import assert from 'node:assert/strict';
import { NAV_SECTIONS, navSection } from './nav.ts';

test('nav: exactly 7 sections', () => {
  assert.equal(NAV_SECTIONS.length, 7);
});

test('nav: numbers are zero-padded and sequential from 01', () => {
  NAV_SECTIONS.forEach((section, index) => {
    assert.equal(section.number, String(index + 1).padStart(2, '0'));
  });
});

test('nav: hrefs are unique', () => {
  const hrefs = NAV_SECTIONS.map((section) => section.href);
  assert.equal(new Set(hrefs).size, hrefs.length);
});

test('navSection: returns the entry for a known href', () => {
  const entry = navSection('/videos');
  assert.equal(entry.number, '05');
  assert.equal(entry.label, 'Biblioteca');
});
