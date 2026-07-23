export interface NativeApprovalPrompt {
  actionId: string;
  description?: string;
  fields: readonly { label: string; value: string }[];
  operation: string;
  risk: 'costly' | 'destructive' | 'write';
  summary?: string;
  title: string;
}

export interface NativeApprovalTarget {
  approvalPrompt(actionId: string): NativeApprovalPrompt;
  approve(actionId: string): Promise<void>;
}

export type NativeApprovalDecision = 'approve' | 'cancel';
export type ShowNativeApproval = (prompt: NativeApprovalPrompt) => Promise<NativeApprovalDecision>;

/**
 * The content renderer can request this gate, but only the main-owned native
 * confirmation result can advance pending executable state.
 */
export class NativeApprovalGate {
  readonly #inFlight = new Set<string>();
  readonly #show: ShowNativeApproval;

  constructor(show: ShowNativeApproval) {
    this.#show = show;
  }

  async request(actionId: string, target: NativeApprovalTarget): Promise<void> {
    if (this.#inFlight.has(actionId)) throw new Error('native approval is already open for this action');
    const prompt = target.approvalPrompt(actionId);
    this.#inFlight.add(actionId);
    try {
      const decision = await this.#show(prompt);
      if (decision === 'approve') await target.approve(actionId);
    } finally {
      this.#inFlight.delete(actionId);
    }
  }
}

export function nativeApprovalDetail(prompt: NativeApprovalPrompt): string {
  const lines = [
    `Riesgo: ${nativeRiskLabel(prompt.risk)}`,
    `Operación: ${prompt.operation}`,
  ];
  if (prompt.description !== undefined) lines.push('', prompt.description);
  if (prompt.summary !== undefined) lines.push('', prompt.summary);
  if (prompt.fields.length > 0) {
    lines.push('', ...prompt.fields.map((field) => `${field.label}: ${field.value}`));
  }
  lines.push('', 'Esta aprobación sólo puede concederse desde este diálogo nativo de FragForge Studio.');
  return lines.join('\n');
}

function nativeRiskLabel(risk: NativeApprovalPrompt['risk']): string {
  switch (risk) {
    case 'costly':
      return 'acción costosa';
    case 'destructive':
      return 'acción destructiva';
    case 'write':
      return 'cambio local';
  }
}
