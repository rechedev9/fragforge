import {
  ASSISTANT_ACTION,
  parseAssistantRequest,
  type AssistantContext,
  type AssistantIPCResponse,
  type AssistantSnapshot,
} from './assistant-ipc.ts';

export interface AssistantCommandController {
  approve(actionID: string): Promise<void>;
  cancel(): Promise<void>;
  clearHistory(): Promise<void>;
  newConversation(): Promise<void>;
  reject(actionID: string): void;
  send(message: string, context: AssistantContext): Promise<void>;
  snapshot(): AssistantSnapshot;
  status(): Promise<AssistantSnapshot>;
}

export function assistantCommandFailure(error: string, snapshot?: AssistantSnapshot): AssistantIPCResponse {
  return { error, ok: false, ...(snapshot === undefined ? {} : { snapshot }) };
}

/** Executes the parsed narrow assistant command and always terminates mutations with an explicit result. */
export async function dispatchAssistantRequest(
  value: unknown,
  getController: () => AssistantCommandController,
): Promise<AssistantIPCResponse> {
  let request;
  try {
    request = parseAssistantRequest(value);
  } catch {
    return assistantCommandFailure('Solicitud del asistente no válida.');
  }

  let controller: AssistantCommandController;
  try {
    controller = getController();
  } catch {
    return assistantCommandFailure('No se pudo iniciar el asistente.');
  }

  try {
    switch (request.action) {
      case ASSISTANT_ACTION.status:
        return { ok: true, snapshot: await controller.status() };
      case ASSISTANT_ACTION.send:
        await controller.send(request.message, request.context);
        break;
      case ASSISTANT_ACTION.cancel:
        await controller.cancel();
        break;
      case ASSISTANT_ACTION.approve:
        await controller.approve(request.actionId);
        break;
      case ASSISTANT_ACTION.reject:
        controller.reject(request.actionId);
        break;
      case ASSISTANT_ACTION.newConversation:
        await controller.newConversation();
        break;
      case ASSISTANT_ACTION.clear:
        await controller.clearHistory();
        break;
    }
    return { ok: true, snapshot: controller.snapshot() };
  } catch {
    let snapshot: AssistantSnapshot | undefined;
    try {
      snapshot = controller.snapshot();
    } catch {
      // The command result must remain terminal even if diagnostic state is unavailable.
    }
    return assistantCommandFailure('No se pudo completar la operación del asistente.', snapshot);
  }
}
