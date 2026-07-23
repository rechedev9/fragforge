import { randomBytes } from 'node:crypto';

const CAPABILITY_BYTES = 32;
const CAPABILITY_PATTERN = /^[a-f0-9]{64}$/;

export const PROXY_CAPABILITY_COOKIE = 'fragforge_proxy_capability';

export interface BootSecurityCapabilities {
  discoverySecret: string;
  mutationToken: string;
  proxyMutationCapability: string;
}

export interface CookieStore {
  set(details: {
    httpOnly: boolean;
    name: string;
    path: string;
    sameSite: 'strict';
    secure: boolean;
    url: string;
    value: string;
  }): Promise<void>;
}

type CapabilityGenerator = () => string;

/** Creates independent, ephemeral capabilities for discovery, API auth, and the renderer proxy. */
export function createBootSecurityCapabilities(
  generate: CapabilityGenerator = generateCapability,
): BootSecurityCapabilities {
  const capabilities = {
    discoverySecret: generate(),
    mutationToken: generate(),
    proxyMutationCapability: generate(),
  };
  const values = Object.values(capabilities);
  if (values.some((value) => !CAPABILITY_PATTERN.test(value))) {
    throw new Error('boot capability generator returned an invalid value');
  }
  if (new Set(values).size !== values.length) {
    throw new Error('boot security capabilities must be distinct');
  }
  return capabilities;
}

export function orchestratorSecurityEnvironment(
  capabilities: BootSecurityCapabilities,
): NodeJS.ProcessEnv {
  return {
    FRAGFORGE_PROXY_BOOTSTRAP_CAPABILITY: undefined,
    FRAGFORGE_PROXY_MUTATION_CAPABILITY: undefined,
    ORCHESTRATOR_TOKEN: undefined,
    ZV_DISCOVERY_SECRET: capabilities.discoverySecret,
    ZV_MUTATION_TOKEN: capabilities.mutationToken,
  };
}

export function webSecurityEnvironment(
  capabilities: BootSecurityCapabilities,
): NodeJS.ProcessEnv {
  return {
    FRAGFORGE_PROXY_BOOTSTRAP_CAPABILITY: undefined,
    FRAGFORGE_PROXY_MUTATION_CAPABILITY: capabilities.proxyMutationCapability,
    ORCHESTRATOR_TOKEN: capabilities.mutationToken,
    ZV_DISCOVERY_SECRET: undefined,
    ZV_MUTATION_TOKEN: undefined,
  };
}

/** Seeds the HttpOnly capability before the first renderer navigation to the local web origin. */
export async function installProxyCapabilityCookie(
  cookies: CookieStore,
  webOrigin: string,
  capability: string,
): Promise<void> {
  if (!CAPABILITY_PATTERN.test(capability)) throw new Error('invalid proxy mutation capability');
  const origin = new URL(webOrigin);
  if (origin.protocol !== 'http:' || origin.hostname !== '127.0.0.1' || origin.pathname !== '/') {
    throw new Error('proxy capability cookie requires the explicit HTTP loopback origin');
  }
  await cookies.set({
    httpOnly: true,
    name: PROXY_CAPABILITY_COOKIE,
    path: '/',
    sameSite: 'strict',
    secure: false,
    url: origin.origin,
    value: capability,
  });
}

function generateCapability(): string {
  return randomBytes(CAPABILITY_BYTES).toString('hex');
}
