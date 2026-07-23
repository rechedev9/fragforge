import {
  ASSISTANT_ACTION,
  parseAssistantRequest,
  type AssistantContext,
  type AssistantIPCResponse,
  type AssistantSnapshot,
} from './assistant-ipc.ts';
import type { NativeApprovalTarget } from './assistant/native-approval.ts';

export interface AssistantCommandController extends NativeApprovalTarget {
  cancel(): Promise<void>;
  clearHistory(): Promise<void>;
  login(): Promise<void>;
  logout(): Promise<void>;
  newConversation(): Promise<void>;
  reject(actionID: string): void;
  send(message: string, context: AssistantContext): Promise<void>;
  snapshot(): AssistantSnapshot;
  status(): Promise<AssistantSnapshot>;
  wake(): Promise<void>;
}

export function assistantCommandFailure(error: string, snapshot?: AssistantSnapshot): AssistantIPCResponse {
  return { error, ok: false, ...(snapshot === undefined ? {} : { snapshot }) };
}

/** Executes the parsed narrow assistant command and always terminates mutations with an explicit result. */
export async function dispatchAssistantRequest(
  value: unknown,
  getController: () => AssistantCommandController,
  requestNativeApproval?: (
    actionId: string,
    controller: AssistantCommandController,
  ) => Promise<void>,
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
      case ASSISTANT_ACTION.wake:
        await controller.wake();
        break;
      case ASSISTANT_ACTION.send:
        await controller.send(request.message, request.context);
        break;
      case ASSISTANT_ACTION.cancel:
        await controller.cancel();
        break;
      case ASSISTANT_ACTION.requestApproval:
        if (requestNativeApproval === undefined) throw new Error('native approval is unavailable');
        await requestNativeApproval(request.actionId, controller);
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
      case ASSISTANT_ACTION.login:
        await controller.login();
        break;
      case ASSISTANT_ACTION.logout:
        await controller.logout();
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
