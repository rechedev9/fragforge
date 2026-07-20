import { discoverDynamicInputs, listOperations, operationNamed, type OperationDefinition, type OperationRisk } from './operations.ts';
import { isJsonObject, type JsonObject, type JsonValue } from './json.ts';
import { OrchestratorClient } from './orchestrator-client.ts';

const DEFAULT_SEARCH_LIMIT = 8;
const MAX_SEARCH_LIMIT = 20;

export interface SearchRequest {
  arguments: JsonObject;
  category?: string;
  includeDynamicInputs: boolean;
  limit: number;
  operation?: string;
  query: string;
  risk?: OperationRisk;
}

export function parseSearchRequest(input: JsonObject): SearchRequest {
  const query = typeof input.query === 'string' ? input.query.trim() : '';
  const operation = typeof input.operation === 'string' && input.operation !== '' ? input.operation : undefined;
  const category = typeof input.category === 'string' && input.category !== '' ? input.category : undefined;
  const risk = parseRisk(input.risk);
  const requestedLimit = typeof input.limit === 'number' && Number.isInteger(input.limit) ? input.limit : DEFAULT_SEARCH_LIMIT;
  if (requestedLimit < 1 || requestedLimit > MAX_SEARCH_LIMIT) throw new Error(`limit must be from 1 to ${MAX_SEARCH_LIMIT}`);
  const argumentsValue = input.arguments;
  if (argumentsValue !== undefined && !isJsonObject(argumentsValue)) throw new Error('arguments must be an object');
  return {
    arguments: argumentsValue ?? {},
    category,
    includeDynamicInputs: input.include_dynamic_inputs !== false,
    limit: requestedLimit,
    operation,
    query,
    risk,
  };
}

export async function searchOperationCatalog(client: OrchestratorClient, request: SearchRequest, signal?: AbortSignal): Promise<JsonObject> {
  let matches: OperationDefinition[];
  if (request.operation !== undefined) {
    const exact = operationNamed(request.operation);
    matches = exact === undefined
      || (request.category !== undefined && exact.category !== request.category)
      || (request.risk !== undefined && exact.risk !== request.risk)
      ? []
      : [exact];
  } else {
    matches = listOperations()
      .filter((operation) => request.category === undefined || operation.category === request.category)
      .filter((operation) => request.risk === undefined || operation.risk === request.risk)
      .map((operation) => ({ operation, score: searchScore(operation, request.query) }))
      .filter((entry) => request.query === '' || entry.score > 0)
      .sort((left, right) => right.score - left.score || left.operation.name.localeCompare(right.operation.name))
      .slice(0, request.limit)
      .map((entry) => entry.operation);
  }

  const descriptors = await Promise.all(matches.slice(0, request.limit).map(async (operation) => {
    const descriptor = operationDescriptor(operation);
    if (!request.includeDynamicInputs) return descriptor;
    try {
      const dynamicInputs = await discoverDynamicInputsIndependently(client, operation, request.arguments, signal);
      descriptor.dynamic_inputs = dynamicInputs;
      const unavailableFields = dynamicInputs
        .filter((field) => field.unavailable === true && typeof field.field === 'string')
        .map((field) => field.field);
      if (unavailableFields.length > 0) {
        descriptor.dynamic_inputs_error = `dynamic inputs unavailable for: ${unavailableFields.join(', ')}`;
      }
    } catch (error: unknown) {
      if (signal?.aborted) throw error;
      descriptor.dynamic_inputs = [];
      descriptor.dynamic_inputs_error = error instanceof Error ? error.message : String(error);
    }
    return descriptor;
  }));

  return {
    count: descriptors.length,
    instructions: 'Choose one exact operation, fill arguments using its input_schema and dynamic_inputs, then call execute. Mutations default to preview and require mode=apply plus confirmed=true.',
    operations: descriptors,
  };
}

export function operationCatalogResource(): JsonObject {
  return {
    generated_from: 'desktop/src/mcp/operations.ts',
    operations: listOperations().map(operationDescriptor),
    pattern: 'The integrated agent uses progressive disclosure: it searches allowlisted Studio operations and receives schemas on demand.',
  };
}

function operationDescriptor(operation: OperationDefinition): JsonObject {
  return {
    category: operation.category,
    description: operation.description,
    input_schema: operation.inputSchema,
    name: operation.name,
    requires_confirmation: operation.risk !== 'read',
    risk: operation.risk,
    title: operation.title,
  };
}

function searchScore(operation: OperationDefinition, query: string): number {
  if (query === '') return defaultRank(operation);
  const normalizedQuery = searchablePhrase(query);
  const tokens = new Set(searchTokens(query));
  const nameTokens = new Set(searchTokens(operation.name));
  const titleTokens = new Set(searchTokens(operation.title));
  const descriptionTokens = new Set(searchTokens(operation.description));
  const keywordTokens = new Set(operation.keywords.flatMap(searchTokens));
  const aliases = SEARCH_ALIASES[operation.name] ?? [];
  const aliasTokenSets = aliases.map((alias) => new Set(searchTokens(alias)));
  let score = 0;
  if (searchablePhrase(operation.name) === normalizedQuery) score += 300;
  if (searchablePhrase(operation.title) === normalizedQuery) score += 220;
  if (aliases.some((alias) => searchablePhrase(alias) === normalizedQuery)) score += 260;
  for (const token of tokens) {
    if (nameTokens.has(token)) score += 30;
    if (titleTokens.has(token)) score += 22;
    if (keywordTokens.has(token)) score += 16;
    if (descriptionTokens.has(token)) score += 6;
    if (aliasTokenSets.some((aliasTokens) => aliasTokens.has(token))) score += 12;
  }
  const searchableOperationTokens = new Set([
    ...nameTokens,
    ...titleTokens,
    ...descriptionTokens,
    ...keywordTokens,
    ...aliasTokenSets.flatMap((aliasTokens) => [...aliasTokens]),
  ]);
  if (tokens.size > 0 && [...tokens].every((token) => searchableOperationTokens.has(token))) {
    score += 45;
  }
  for (const aliasTokens of aliasTokenSets) {
    if (aliasTokens.size > 1 && [...aliasTokens].every((token) => tokens.has(token))) score += 80;
  }
  if (operation.risk === 'destructive' && ![...tokens].some((token) => DESTRUCTIVE_SEARCH_TERMS.has(token))) {
    score -= 100;
  }
  return score;
}

const DESTRUCTIVE_SEARCH_TERMS = new Set(['borrar', 'delete', 'eliminar', 'erase', 'remove']);

const SEARCH_STOP_WORDS = new Set([
  'a', 'al', 'an', 'and', 'con', 'de', 'del', 'el', 'en', 'for', 'la', 'las', 'los', 'of', 'para', 'por', 'the', 'to', 'un', 'una', 'ver', 'with', 'y',
]);

const SEARCH_ALIASES: Readonly<Record<string, readonly string[]>> = {
  'artifacts.get_song_url': ['descargar cancion', 'descargar musica', 'download song', 'music download'],
  'artifacts.get_url': ['descargar video', 'bajar video', 'download video', 'obtener video', 'video url'],
  'artifacts.get_voice_profile_audio_url': ['audio del perfil de voz', 'descargar voz', 'voice profile audio', 'download voice reference'],
  'catalog.songs': ['lista de canciones', 'listar canciones', 'canciones', 'lista de musica', 'list songs', 'music list'],
  'jobs.create': ['subir una demo', 'subir demo', 'cargar demo', 'crear trabajo', 'upload demo', 'create job', 'import demo'],
  'jobs.list': ['ver trabajos recientes', 'trabajos recientes', 'listar trabajos', 'demos recientes', 'recent jobs', 'list jobs'],
  'jobs.record': ['grabar kills', 'grabar bajas', 'capturar kills', 'record kills', 'capture kills'],
  'studio.metrics': ['ver errores', 'errores', 'fallos', 'metricas', 'studio errors', 'errors', 'metrics'],
  'studio.status': ['estado del estudio', 'estado estudio', 'salud del estudio', 'studio status', 'studio health'],
  'streams.configure_captions': ['subtitulos con grok', 'configurar subtitulos', 'activar subtitulos', 'configure captions', 'enable subtitles'],
  'streams.edit_clip': ['editar clip', 'velocidad del clip', 'camara lenta', 'silenciar clip', 'quitar audio', 'texto en pantalla', 'fundido', 'edit clip', 'clip speed', 'slow motion', 'mute clip', 'add text overlay', 'fade'],
  'voices.delete_profile': ['borrar perfil de voz', 'eliminar perfil de voz', 'delete voice profile'],
  'voices.get_profile': ['perfil de voz', 'ver perfil de voz', 'voice profile'],
  'voices.save_profile': ['subir voz', 'guardar perfil de voz', 'crear perfil de voz', 'upload voice', 'save voice profile'],
};

function defaultRank(operation: OperationDefinition): number {
  const defaults: Record<string, number> = {
    'studio.status': 100,
    'jobs.list': 90,
    'jobs.create': 80,
    'jobs.generate': 70,
    'renders.get': 60,
    'catalog.presets': 50,
  };
  return defaults[operation.name] ?? 1;
}

function normalize(value: string): string {
  return value
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, ' ')
    .trim();
}

function searchTokens(value: string): string[] {
  return normalize(value)
    .split(' ')
    .filter((token) => token !== '' && !SEARCH_STOP_WORDS.has(token));
}

function searchablePhrase(value: string): string {
  return searchTokens(value).join(' ');
}

const DYNAMIC_SCHEMA_FIELDS = ['job_id', 'stream_job_id', 'preset', 'variant', 'music', 'song_id'] as const;

interface DynamicDiscoverySpec {
  field: string;
  input: JsonObject;
  operation: OperationDefinition;
}

async function discoverDynamicInputsIndependently(
  client: OrchestratorClient,
  operation: OperationDefinition,
  input: JsonObject,
  signal?: AbortSignal,
): Promise<JsonObject[]> {
  const specs = dynamicDiscoverySpecs(operation, input);
  const results = await Promise.all(specs.map(async (spec) => {
    try {
      return await discoverDynamicInputs(client, spec.operation, spec.input, signal);
    } catch (error: unknown) {
      if (signal?.aborted) throw error;
      return [{
        error: error instanceof Error ? error.message : String(error),
        field: spec.field,
        unavailable: true,
      }];
    }
  }));
  return results.flat();
}

function dynamicDiscoverySpecs(operation: OperationDefinition, input: JsonObject): DynamicDiscoverySpec[] {
  const specs: DynamicDiscoverySpec[] = [];
  const properties = isJsonObject(operation.inputSchema.properties) ? operation.inputSchema.properties : {};
  for (const field of DYNAMIC_SCHEMA_FIELDS) {
    const property = properties[field];
    if (!isJsonObject(property)) continue;
    const fieldInput: JsonObject = {};
    if (field === 'variant' && typeof input.kind === 'string') fieldInput.kind = input.kind;
    specs.push({
      field,
      input: fieldInput,
      operation: operationWithDynamicField(operation, field, property),
    });
  }

  const emptySchemaOperation = operationWithNoDynamicFields(operation);
  const jobID = typeof input.job_id === 'string' ? input.job_id : undefined;
  if (jobID !== undefined && operation.name === 'jobs.parse') {
    specs.push({ field: 'target_steamid', input, operation: emptySchemaOperation });
  }
  if (jobID !== undefined && (operation.name === 'jobs.record' || operation.name === 'jobs.generate')) {
    specs.push({ field: 'segment_ids', input, operation: emptySchemaOperation });
  }
  if (jobID !== undefined && (operation.name === 'renders.delete_video'
    || operation.name === 'renders.publish_assistant'
    || operation.name === 'artifacts.get_url') && typeof input.variant === 'string') {
    specs.push({ field: 'name', input, operation: emptySchemaOperation });
  }

  const streamJobID = typeof input.stream_job_id === 'string' ? input.stream_job_id : undefined;
  if (streamJobID !== undefined && operation.name === 'streams.update_edit_plan') {
    specs.push({ field: 'plan', input, operation: emptySchemaOperation });
  }
  if (streamJobID !== undefined && operation.name === 'artifacts.get_stream_url'
    && input.kind === 'video' && typeof input.variant === 'string') {
    specs.push({ field: 'clip_id', input, operation: emptySchemaOperation });
  }
  return specs;
}

function operationWithDynamicField(
  operation: OperationDefinition,
  field: string,
  property: JsonObject,
): OperationDefinition {
  return {
    ...operation,
    inputSchema: {
      ...operation.inputSchema,
      properties: { [field]: property },
    },
  };
}

function operationWithNoDynamicFields(operation: OperationDefinition): OperationDefinition {
  return {
    ...operation,
    inputSchema: {
      ...operation.inputSchema,
      properties: {},
    },
  };
}

function parseRisk(value: JsonValue | undefined): OperationRisk | undefined {
  if (value === undefined) return undefined;
  if (value === 'read' || value === 'write' || value === 'costly' || value === 'destructive') return value;
  throw new Error('risk must be read, write, costly, or destructive');
}
