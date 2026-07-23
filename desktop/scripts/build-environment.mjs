/** Copies an environment while dropping every casing variant of the credential. */
export function environmentWithoutXAIAPIKey(environment = process.env) {
  const sanitized = { ...environment };
  for (const name of Object.keys(sanitized)) {
    if (name.toLowerCase() === 'xai_api_key') delete sanitized[name];
  }
  return sanitized;
}

/** Makes the established unsigned release flow deterministic on developer machines. */
export function environmentWithoutCodeSigningCredentials(environment = process.env) {
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
  sanitized.CSC_IDENTITY_AUTO_DISCOVERY = 'false';
  return sanitized;
}
