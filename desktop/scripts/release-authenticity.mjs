import { execFile } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);
const SUBJECT_MAX_LENGTH = 512;
const THUMBPRINT_PATTERN = /^[A-F0-9]{40,128}$/;

const INSPECT_SIGNATURE_SCRIPT = `
$ErrorActionPreference = 'Stop'
$signature = Get-AuthenticodeSignature -LiteralPath $env:FRAGFORGE_INSTALLER_TO_VERIFY
[PSCustomObject]@{
  signatureType = [string]$signature.SignatureType
  signerSubject = if ($null -eq $signature.SignerCertificate) { '' } else { [string]$signature.SignerCertificate.Subject }
  signerThumbprint = if ($null -eq $signature.SignerCertificate) { '' } else { [string]$signature.SignerCertificate.Thumbprint }
  status = [string]$signature.Status
  timestampSubject = if ($null -eq $signature.TimeStamperCertificate) { '' } else { [string]$signature.TimeStamperCertificate.Subject }
} | ConvertTo-Json -Compress
`.trim();

export function requireAuthenticodeSigningConfiguration(environment = process.env) {
  const certificate = firstEnvironmentValue(environment, ['WIN_CSC_LINK', 'CSC_LINK']);
  const password = firstEnvironmentValue(environment, ['WIN_CSC_KEY_PASSWORD', 'CSC_KEY_PASSWORD']);
  const expectedSubject = requireExpectedAuthenticodeSubject(environment);
  if (certificate === undefined) {
    throw new Error('[dist] Authenticode signing requires WIN_CSC_LINK or CSC_LINK');
  }
  if (password === undefined) {
    throw new Error('[dist] Authenticode signing requires WIN_CSC_KEY_PASSWORD or CSC_KEY_PASSWORD');
  }
  return { expectedSubject };
}

export function requireExpectedAuthenticodeSubject(environment = process.env) {
  const expectedSubject = firstEnvironmentValue(environment, ['FRAGFORGE_AUTHENTICODE_SUBJECT']);
  if (expectedSubject === undefined) {
    throw new Error('[dist] FRAGFORGE_AUTHENTICODE_SUBJECT must name the exact expected publisher');
  }
  if (expectedSubject.length > SUBJECT_MAX_LENGTH || /[\r\n\0]/.test(expectedSubject)) {
    throw new Error('[dist] FRAGFORGE_AUTHENTICODE_SUBJECT is invalid');
  }
  return expectedSubject;
}

export function environmentWithoutAuthenticodeCredentials(environment = process.env) {
  const sanitized = { ...environment };
  for (const name of Object.keys(sanitized)) {
    const normalized = name.toUpperCase();
    if (normalized === 'CSC_LINK'
      || normalized === 'CSC_KEY_PASSWORD'
      || normalized === 'WIN_CSC_LINK'
      || normalized === 'WIN_CSC_KEY_PASSWORD') {
      delete sanitized[name];
    }
  }
  return sanitized;
}

export async function verifyAuthenticodeSignature(
  installerPath,
  expectedSubject,
  inspect = inspectAuthenticodeSignature,
) {
  const signature = parseSignatureResult(await inspect(installerPath));
  if (signature.status !== 'Valid') {
    throw new Error(`[dist] installer Authenticode status is ${signature.status || 'missing'}`);
  }
  if (signature.signatureType !== 'Authenticode') {
    throw new Error('[dist] installer does not contain a direct Authenticode signature');
  }
  if (signature.signerSubject !== expectedSubject) {
    throw new Error('[dist] installer signer does not match FRAGFORGE_AUTHENTICODE_SUBJECT');
  }
  if (signature.timestampSubject === '') {
    throw new Error('[dist] installer Authenticode signature is not timestamped');
  }
  if (!THUMBPRINT_PATTERN.test(signature.signerThumbprint)) {
    throw new Error('[dist] installer signer thumbprint is invalid');
  }
  return signature;
}

export function writePublisherIdentity(filePath, signature) {
  writeFileSync(filePath, `${JSON.stringify({
    schemaVersion: 1,
    signatureType: signature.signatureType,
    signerSubject: signature.signerSubject,
    signerThumbprint: signature.signerThumbprint,
    timestampSubject: signature.timestampSubject,
  }, null, 2)}\n`, { encoding: 'utf8', mode: 0o600 });
}

export function verifyPublisherIdentity(filePath, signature) {
  let value;
  try {
    value = JSON.parse(readFileSync(filePath, 'utf8'));
  } catch {
    throw new Error('[dist] AUTHENTICODE_PUBLISHER.json is invalid');
  }
  const expected = {
    schemaVersion: 1,
    signatureType: signature.signatureType,
    signerSubject: signature.signerSubject,
    signerThumbprint: signature.signerThumbprint,
    timestampSubject: signature.timestampSubject,
  };
  if (JSON.stringify(value) !== JSON.stringify(expected)) {
    throw new Error('[dist] AUTHENTICODE_PUBLISHER.json does not match the installer signature');
  }
}

async function inspectAuthenticodeSignature(installerPath) {
  const environment = verificationEnvironment(installerPath);
  const encodedScript = Buffer.from(INSPECT_SIGNATURE_SCRIPT, 'utf16le').toString('base64');
  const { stdout } = await execFileAsync(
    'powershell.exe',
    ['-NoProfile', '-NonInteractive', '-EncodedCommand', encodedScript],
    {
      encoding: 'utf8',
      env: environment,
      timeout: 30_000,
      windowsHide: true,
    },
  );
  return stdout;
}

function parseSignatureResult(output) {
  let value;
  try {
    value = JSON.parse(String(output).trim());
  } catch {
    throw new Error('[dist] could not parse the Authenticode verification result');
  }
  if (!isRecord(value)
    || typeof value.signatureType !== 'string'
    || typeof value.signerSubject !== 'string'
    || typeof value.signerThumbprint !== 'string'
    || typeof value.status !== 'string'
    || typeof value.timestampSubject !== 'string') {
    throw new Error('[dist] Authenticode verification returned an invalid result');
  }
  return {
    signatureType: value.signatureType,
    signerSubject: value.signerSubject,
    signerThumbprint: value.signerThumbprint.toUpperCase(),
    status: value.status,
    timestampSubject: value.timestampSubject,
  };
}

function verificationEnvironment(installerPath) {
  const environment = environmentWithoutAuthenticodeCredentials(process.env);
  environment.FRAGFORGE_INSTALLER_TO_VERIFY = installerPath;
  return environment;
}

function firstEnvironmentValue(environment, names) {
  for (const wanted of names) {
    const found = Object.entries(environment)
      .find(([name, value]) => name.toUpperCase() === wanted && typeof value === 'string'
        && value.trim() !== '');
    if (found !== undefined) return found[1].trim();
  }
  return undefined;
}

function isRecord(value) {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
