import { randomUUID } from 'node:crypto';
import * as fs from 'node:fs/promises';
import * as path from 'node:path';
import { normalizeXAIAPIKey } from './xai-api-key.ts';

const MAX_ENCRYPTED_BLOB_BYTES = 64 * 1024;
const ENCRYPTION_UNAVAILABLE_MESSAGE =
  'secure xAI API key storage is unavailable on this machine';

export interface DecryptedXAIAPIKey {
  value: string;
  shouldReEncrypt: boolean;
}

/** Adapter implemented by Electron safeStorage in the main process. */
export interface XAIAPIKeyCodec {
  isAvailable(): Promise<boolean>;
  encrypt(value: string): Promise<Buffer>;
  decrypt(encrypted: Buffer): Promise<DecryptedXAIAPIKey>;
}

export interface XAIAPIKeyStoreOptions {
  codec: XAIAPIKeyCodec;
  filePath: string;
}

/**
 * Persists one user-provided xAI credential as an OS-encrypted blob. Operations
 * are serialized so a key rotation discovered during load cannot overwrite a
 * newer save from another settings action.
 */
export class XAIAPIKeyStore {
  readonly #codec: XAIAPIKeyCodec;
  readonly #filePath: string;
  #operationTail: Promise<void> = Promise.resolve();

  constructor(options: XAIAPIKeyStoreOptions) {
    this.#codec = options.codec;
    this.#filePath = options.filePath;
  }

  /** Reports availability without allowing a codec failure to expose details. */
  async isAvailable(): Promise<boolean> {
    try {
      return await this.#codec.isAvailable();
    } catch {
      return false;
    }
  }

  /** Returns the normalized key, or undefined when no per-user key is saved. */
  async load(): Promise<string | undefined> {
    return this.#exclusive(async () => {
      const encrypted = await this.#readEncryptedBlob();
      if (encrypted === undefined) return undefined;
      await this.#requireEncryption();

      let decrypted: DecryptedXAIAPIKey;
      try {
        decrypted = await this.#codec.decrypt(encrypted);
      } catch {
        throw new Error('could not decrypt the saved xAI API key');
      }
      const value = normalizeXAIAPIKey(decrypted.value);
      if (decrypted.shouldReEncrypt) await this.#persist(value);
      return value;
    });
  }

  /** Encrypts and atomically creates or replaces the saved credential. */
  async save(value: string): Promise<void> {
    const normalized = normalizeXAIAPIKey(value);
    await this.#exclusive(async () => {
      await this.#requireEncryption();
      await this.#persist(normalized);
    });
  }

  /** Removes the encrypted blob. Returns whether one existed. */
  async remove(): Promise<boolean> {
    return this.#exclusive(async () => {
      try {
        await fs.rm(this.#filePath);
        return true;
      } catch (err) {
        if (isMissingFileError(err)) return false;
        throw new Error('could not remove the saved xAI API key');
      }
    });
  }

  async #requireEncryption(): Promise<void> {
    if (!(await this.isAvailable())) throw new Error(ENCRYPTION_UNAVAILABLE_MESSAGE);
  }

  async #readEncryptedBlob(): Promise<Buffer | undefined> {
    let handle: fs.FileHandle;
    try {
      handle = await fs.open(this.#filePath, 'r');
    } catch (err) {
      if (isMissingFileError(err)) return undefined;
      throw new Error('could not read the saved xAI API key');
    }

    try {
      const bounded = Buffer.alloc(MAX_ENCRYPTED_BLOB_BYTES + 1);
      const { bytesRead } = await handle.read(
        bounded,
        0,
        bounded.length,
        0,
      );
      if (bytesRead === 0 || bytesRead > MAX_ENCRYPTED_BLOB_BYTES) {
        throw new Error('saved xAI API key data is invalid');
      }
      return Buffer.from(bounded.subarray(0, bytesRead));
    } catch (err) {
      if (err instanceof Error && err.message === 'saved xAI API key data is invalid') throw err;
      throw new Error('could not read the saved xAI API key');
    } finally {
      await handle.close().catch(() => {});
    }
  }

  async #persist(value: string): Promise<void> {
    let encrypted: Buffer;
    try {
      encrypted = await this.#codec.encrypt(value);
    } catch {
      throw new Error('could not encrypt the xAI API key');
    }
    if (
      !Buffer.isBuffer(encrypted)
      || encrypted.length === 0
      || encrypted.length > MAX_ENCRYPTED_BLOB_BYTES
    ) {
      throw new Error('encrypted xAI API key data is invalid');
    }

    const directory = path.dirname(this.#filePath);
    const temporary = path.join(
      directory,
      `.${path.basename(this.#filePath)}.${process.pid}.${randomUUID()}.tmp`,
    );
    let handle: fs.FileHandle | undefined;
    try {
      await fs.mkdir(directory, { recursive: true, mode: 0o700 });
      handle = await fs.open(temporary, 'wx', 0o600);
      await handle.writeFile(encrypted);
      await handle.sync();
      await handle.close();
      handle = undefined;
      await fs.rename(temporary, this.#filePath);
    } catch {
      if (handle !== undefined) await handle.close().catch(() => {});
      await fs.rm(temporary, { force: true }).catch(() => {});
      throw new Error('could not save the encrypted xAI API key');
    }
  }

  #exclusive<T>(operation: () => Promise<T>): Promise<T> {
    const result = this.#operationTail.then(operation, operation);
    this.#operationTail = result.then(
      () => undefined,
      () => undefined,
    );
    return result;
  }
}

function isMissingFileError(err: unknown): boolean {
  return err instanceof Error && 'code' in err && err.code === 'ENOENT';
}
