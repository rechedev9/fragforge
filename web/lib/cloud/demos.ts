import type { SupabaseClient } from '@supabase/supabase-js';
// Relative .ts import (not the `@/` alias) so Node's native TS loader can resolve
// it when demos.test.ts runs this module directly under node --test.
import { JOB_STATE, JOB_TYPE } from './jobDto.ts';

/** Upload the demo and queue a scan job. Returns the demo id (the browser handle). */
export async function createScanJob(
  userId: string,
  file: { name: string; size: number; bytes: ArrayBuffer },
  db?: SupabaseClient,
): Promise<{ demoId: string }> {
  const client = db ?? (await import('@/lib/supabase/server')).supabaseAdmin();
  const demoId = crypto.randomUUID();
  const key = `demos/${userId}/${demoId}.dem`;
  // Direct service-role upload into the private demos bucket.
  const put = await client.storage.from('demos').upload(`${userId}/${demoId}.dem`, file.bytes, {
    contentType: 'application/octet-stream',
    upsert: false,
  });
  if (put.error) throw new Error(`upload: ${put.error.message}`);

  const demo = await client
    .from('demos')
    .insert({ id: demoId, user_id: userId, storage_key: key, filename: file.name, size: file.size });
  if (demo.error) throw new Error(`insert demo: ${demo.error.message}`);

  const job = await client.from('jobs').insert({ demo_id: demoId, user_id: userId, type: JOB_TYPE.scan, state: JOB_STATE.queued });
  if (job.error) throw new Error(`insert job: ${job.error.message}`);
  return { demoId };
}
