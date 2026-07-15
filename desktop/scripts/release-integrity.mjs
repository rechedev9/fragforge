import { createHash, timingSafeEqual } from 'node:crypto';
import { createReadStream, readFileSync, renameSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { basename, join } from 'node:path';

const CHECKSUM_LINE = /^([a-f0-9]{64})  ([^\r\n]+)$/;

export function releasePaths(desktopDirectory) {
  const outputDirectory = join(desktopDirectory, 'dist-installer');
  const metadata = JSON.parse(readFileSync(join(desktopDirectory, 'package.json'), 'utf8'));
  const installerName = `FragForge Studio Setup ${metadata.version}.exe`;
  return {
    artifacts: [
      join(outputDirectory, installerName),
      join(outputDirectory, `${installerName}.blockmap`),
    ],
    checksum: join(outputDirectory, 'SHA256SUMS.txt'),
  };
}

export async function writeReleaseChecksums(artifacts, checksumPath) {
  if (artifacts.length === 0) throw new Error('release integrity requires at least one artifact');
  const lines = [];
  for (const artifact of artifacts) {
    const name = safeArtifactName(artifact);
    const info = statSync(artifact);
    if (!info.isFile() || info.size === 0) throw new Error(`release artifact is empty: ${name}`);
    lines.push(`${await sha256File(artifact)}  ${name}`);
  }

  const temporary = `${checksumPath}.tmp`;
  rmSync(temporary, { force: true });
  try {
    writeFileSync(temporary, `${lines.join('\n')}\n`, { encoding: 'utf8', mode: 0o600 });
    renameSync(temporary, checksumPath);
  } catch (err) {
    rmSync(temporary, { force: true });
    throw err;
  }
}

export async function verifyReleaseChecksums(artifacts, checksumPath) {
  const expected = new Map();
  for (const artifact of artifacts) {
    const name = safeArtifactName(artifact);
    if (expected.has(name)) throw new Error(`duplicate release artifact name: ${name}`);
    expected.set(name, artifact);
  }

  const declared = new Map();
  const lines = readFileSync(checksumPath, 'utf8').trimEnd().split('\n');
  for (const line of lines) {
    const match = CHECKSUM_LINE.exec(line);
    if (match === null) throw new Error('invalid SHA256SUMS.txt line');
    const [, want, name] = match;
    if (!expected.has(name) || declared.has(name)) throw new Error(`unexpected checksum entry: ${name}`);
    declared.set(name, want);
  }
  if (declared.size !== expected.size) throw new Error('SHA256SUMS.txt is missing a release artifact');

  for (const [name, artifact] of expected) {
    const got = await sha256File(artifact);
    if (!digestMatches(got, declared.get(name))) throw new Error(`sha256 mismatch: ${name}`);
  }
}

async function sha256File(filePath) {
  const hash = createHash('sha256');
  for await (const chunk of createReadStream(filePath)) hash.update(chunk);
  return hash.digest('hex');
}

function safeArtifactName(filePath) {
  const name = basename(filePath);
  if (name === '' || /[\r\n]/.test(name)) throw new Error('invalid release artifact name');
  return name;
}

function digestMatches(got, want) {
  if (!/^[a-f0-9]{64}$/.test(got) || !/^[a-f0-9]{64}$/.test(want)) return false;
  return timingSafeEqual(Buffer.from(got, 'hex'), Buffer.from(want, 'hex'));
}
