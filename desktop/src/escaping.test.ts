import test from 'node:test';
import assert from 'node:assert/strict';
import { escapeHtml, psQuote } from './escaping.ts';

test('escapeHtml escapes all five HTML-sensitive characters', () => {
  assert.equal(escapeHtml('&<>"\''), '&amp;&lt;&gt;&quot;&#39;');
});

test('escapeHtml escapes ampersand before the entities it introduces (no double-escape)', () => {
  // The & rule runs first so a literal < becomes &lt;, not &amp;lt;.
  assert.equal(escapeHtml('<script>'), '&lt;script&gt;');
  assert.equal(escapeHtml('a & b'), 'a &amp; b');
});

test('escapeHtml leaves plain text untouched', () => {
  assert.equal(escapeHtml('hola mundo 123'), 'hola mundo 123');
});

test('escapeHtml stringifies non-string input', () => {
  assert.equal(escapeHtml(42), '42');
  assert.equal(escapeHtml(undefined), 'undefined');
  assert.equal(escapeHtml(new Error('a<b')), 'Error: a&lt;b');
});

test('psQuote wraps in single quotes and doubles embedded quotes', () => {
  assert.equal(psQuote('plain'), "'plain'");
  assert.equal(psQuote("it's"), "'it''s'");
  assert.equal(psQuote(''), "''");
});

test('psQuote leaves a path without quotes intact', () => {
  assert.equal(psQuote('C:\\Users\\me\\tools'), "'C:\\Users\\me\\tools'");
});
