/** Copies an environment while dropping every casing variant of the credential. */
export function environmentWithoutXAIAPIKey(environment = process.env) {
  const sanitized = { ...environment };
  for (const name of Object.keys(sanitized)) {
    if (name.toLowerCase() === 'xai_api_key') delete sanitized[name];
  }
  return sanitized;
}
