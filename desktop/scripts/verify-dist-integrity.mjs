import { dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { releasePaths, verifyReleaseChecksums } from './release-integrity.mjs';
import {
  requireExpectedAuthenticodeSubject,
  verifyAuthenticodeSignature,
  verifyPublisherIdentity,
} from './release-authenticity.mjs';

const desktop = dirname(dirname(fileURLToPath(import.meta.url)));
const { artifacts, checksum, publisher } = releasePaths(desktop);
const expectedSubject = requireExpectedAuthenticodeSubject();
const signature = await verifyAuthenticodeSignature(artifacts[0], expectedSubject);
verifyPublisherIdentity(publisher, signature);

await verifyReleaseChecksums([...artifacts, publisher], checksum);
console.log('[dist] Authenticode publisher and SHA256SUMS.txt verified');
