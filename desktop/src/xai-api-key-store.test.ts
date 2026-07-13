import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import {
  XAIAPIKeyStore,
  type DecryptedXAIAPIKey,
  type XAIAPIKeyCodec,
} from './xai-api-key-store.ts';

const MAX_ENCRYPTED_BLOB_BYTES = 64 * 1024;
const MASK = 0xa5;

class FakeCodec implements XAIAPIKeyCodec {
  available = true;
  decryptCalls = 0;
  encryptCalls = 0;
  reEncryptNextLoad = false;

  async isAvailable(): Promise<boolean> {
    return this.available;
  }

  async encrypt(value: string): Promise<Buffer> {
    this.encryptCalls += 1;
    const plain = Buffer.from(value, 'utf8');
    return Buffer.from(plain.map((byte) => byte ^ MASK));
  }

  async decrypt(encrypted: Buffer): Promise<DecryptedXAIAPIKey> {
    this.decryptCalls += 1;
    const shouldReEncrypt = this.reEncryptNextLoad;
    this.reEncryptNextLoad = false;
    const plain = Buffer.from(encrypted.map((byte) => byte ^ MASK));
    return { value: plain.toString('utf8'), shouldReEncrypt };
  }
}

function testStore(t: test.TestContext): {
  codec: FakeCodec;
  directory: string;
  filePath: string;
  store: XAIAPIKeyStore;
} {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'fragforge-xai-store-'));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const filePath = path.join(directory, 'credentials', 'xai-api-key.bin');
  const codec = new FakeCodec();
  return {
    codec,
    directory,
    filePath,
    store: new XAIAPIKeyStore({ codec, filePath }),
  };
}

test('missing storage loads as unconfigured and removal is idempotent', async (t) => {
  const { codec, store } = testStore(t);

  assert.equal(await store.load(), undefined);
  assert.equal(await store.remove(), false);
  assert.equal(codec.decryptCalls, 0);
});

test('saves, loads, replaces, and removes only encrypted bytes', async (t) => {
  const { filePath, store } = testStore(t);
  const first = 'xai-canary-first';
  const second = 'xai-canary-second';

  await store.save(`  ${first}  `);
  const firstBlob = fs.readFileSync(filePath);
  assert.equal(firstBlob.includes(Buffer.from(first)), false);
  assert.equal(await store.load(), first);

  await store.save(second);
  const secondBlob = fs.readFileSync(filePath);
  assert.equal(secondBlob.includes(Buffer.from(second)), false);
  assert.notDeepEqual(secondBlob, firstBlob);
  assert.equal(await store.load(), second);
  assert.equal(await store.remove(), true);
  assert.equal(fs.existsSync(filePath), false);
});

test('fails closed when OS encryption is unavailable', async (t) => {
  const { codec, filePath, store } = testStore(t);
  codec.available = false;

  assert.equal(await store.isAvailable(), false);
  await assert.rejects(store.save('must-not-be-plaintext'), /secure xAI API key storage is unavailable/);
  assert.equal(fs.existsSync(filePath), false);

  codec.available = true;
  await store.save('encrypted-key');
  codec.available = false;
  await assert.rejects(store.load(), /secure xAI API key storage is unavailable/);
});

test('rejects empty and oversized encrypted blobs before decrypting', async (t) => {
  const { codec, filePath, store } = testStore(t);
  fs.mkdirSync(path.dirname(filePath), { recursive: true });

  fs.writeFileSync(filePath, Buffer.alloc(0));
  await assert.rejects(store.load(), /saved xAI API key data is invalid/);
  fs.writeFileSync(filePath, Buffer.alloc(MAX_ENCRYPTED_BLOB_BYTES + 1, 1));
  await assert.rejects(store.load(), /saved xAI API key data is invalid/);
  assert.equal(codec.decryptCalls, 0);
});

test('rejects invalid decrypted content without echoing it', async (t) => {
  const { codec, filePath, store } = testStore(t);
  const unsafe = 'first-line\nprivate-second-line';
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
  fs.writeFileSync(filePath, await codec.encrypt(unsafe));

  await assert.rejects(
    store.load(),
    (err: unknown) => err instanceof Error
      && /single non-empty line/.test(err.message)
      && !err.message.includes(unsafe),
  );
});

test('re-encrypts a loaded key when the OS codec requests rotation', async (t) => {
  const { codec, filePath, store } = testStore(t);
  await store.save('rotating-key');
  const before = fs.readFileSync(filePath);
  codec.reEncryptNextLoad = true;

  assert.equal(await store.load(), 'rotating-key');
  assert.equal(codec.encryptCalls, 2);
  assert.deepEqual(fs.readFileSync(filePath), before);
});

test('cleans its staged file when atomic publication fails', async (t) => {
  const { filePath, store } = testStore(t);
  fs.mkdirSync(filePath, { recursive: true });

  await assert.rejects(store.save('encrypted-key'), /could not save the encrypted xAI API key/);
  const staged = fs.readdirSync(path.dirname(filePath)).filter((name) => name.endsWith('.tmp'));
  assert.deepEqual(staged, []);
});
