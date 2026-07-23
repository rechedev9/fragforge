import { dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { releasePaths, verifyReleaseChecksums } from './release-integrity.mjs';

const desktop = dirname(dirname(fileURLToPath(import.meta.url)));
const { artifacts, checksum } = releasePaths(desktop);

await verifyReleaseChecksums(artifacts, checksum);
console.log('[dist] SHA256SUMS.txt verified');
