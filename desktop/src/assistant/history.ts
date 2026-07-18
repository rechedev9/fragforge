import { mkdir, readFile, rename, rm, writeFile } from 'node:fs/promises';
import * as path from 'node:path';
import type { AssistantMessage } from '../assistant-ipc.ts';

const HISTORY_VERSION = 1;
const MAX_HISTORY_MESSAGES = 200;
const MAX_MESSAGE_CONTENT_LENGTH = 12_000;

export interface AssistantHistory {
  messages: AssistantMessage[];
  threadId?: string;
}

interface StoredAssistantHistory extends AssistantHistory {
  version: number;
}

/**
 * Local, non-secret display history for the Studio rail.
 *
 * Codex owns its own session data. This file intentionally contains only the
 * bounded message transcript and the opaque thread id needed to resume it.
 */
export class AssistantHistoryStore {
  readonly #filePath: string;
  #pendingWrite: Promise<void> = Promise.resolve();

  constructor(filePath: string) {
    this.#filePath = filePath;
  }

  async load(): Promise<AssistantHistory> {
    try {
      const parsed: unknown = JSON.parse(await readFile(this.#filePath, 'utf8'));
      return parseStoredHistory(parsed);
    } catch {
      return { messages: [] };
    }
  }

  save(history: AssistantHistory): Promise<void> {
    const stored: StoredAssistantHistory = {
      messages: sanitizeMessages(history.messages),
      ...(isThreadID(history.threadId) ? { threadId: history.threadId } : {}),
      version: HISTORY_VERSION,
    };
    return this.#enqueue(async () => {
      await mkdir(path.dirname(this.#filePath), { recursive: true });
      const temporaryPath = `${this.#filePath}.${process.pid}.tmp`;
      await writeFile(temporaryPath, JSON.stringify(stored), { encoding: 'utf8', mode: 0o600 });
      await rename(temporaryPath, this.#filePath);
    });
  }

  clear(): Promise<void> {
    return this.#enqueue(async () => {
      try {
        await rm(this.#filePath, { force: true });
      } catch {
        // The in-memory transcript is already cleared. A stale local display
        // file is not worth turning a user's clear action into an app error.
      }
    });
  }

  #enqueue(write: () => Promise<void>): Promise<void> {
    const next = this.#pendingWrite.then(write, write);
    this.#pendingWrite = next.catch(() => {});
    return next;
  }
}

function parseStoredHistory(value: unknown): AssistantHistory {
  if (!isRecord(value) || value.version !== HISTORY_VERSION || !Array.isArray(value.messages)) return { messages: [] };
  const threadId = isThreadID(value.threadId) ? value.threadId : undefined;
  return {
    messages: sanitizeMessages(value.messages),
    ...(threadId === undefined ? {} : { threadId }),
  };
}

function sanitizeMessages(value: readonly unknown[]): AssistantMessage[] {
  const messages: AssistantMessage[] = [];
  for (const candidate of value.slice(-MAX_HISTORY_MESSAGES)) {
    if (!isRecord(candidate)
      || !isMessageRole(candidate.role)
      || typeof candidate.id !== 'string'
      || candidate.id.length === 0
      || candidate.id.length > 128
      || typeof candidate.createdAt !== 'string'
      || candidate.createdAt.length === 0
      || candidate.createdAt.length > 64
      || typeof candidate.content !== 'string') continue;
    messages.push({
      content: candidate.content.slice(0, MAX_MESSAGE_CONTENT_LENGTH),
      createdAt: candidate.createdAt,
      id: candidate.id,
      role: candidate.role,
    });
  }
  return messages;
}

function isThreadID(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value.length <= 256;
}

function isMessageRole(value: unknown): value is AssistantMessage['role'] {
  return value === 'assistant' || value === 'system' || value === 'user';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
