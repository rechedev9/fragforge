import { contextBridge, ipcRenderer } from 'electron';

// Keep this preload self-contained: sandboxed Electron preloads can import the
// electron module, but must not depend on local CommonJS modules at runtime.
const XAI_SETTINGS_CHANNEL = 'fragforge:xai-settings';
const MCP_CONFIG_CHANNEL = 'fragforge:mcp-config';
const MAX_XAI_API_KEY_INPUT_LENGTH = 4096;

function invokeKeyAction(action: 'save' | 'test', apiKey: string): Promise<unknown> {
  if (typeof apiKey !== 'string' || apiKey.length > MAX_XAI_API_KEY_INPUT_LENGTH) {
    return Promise.reject(new Error('invalid xAI API key input'));
  }
  return ipcRenderer.invoke(XAI_SETTINGS_CHANNEL, { action, apiKey });
}

contextBridge.exposeInMainWorld('fragforgeSettings', {
  getXAIStatus: (): Promise<unknown> => ipcRenderer.invoke(XAI_SETTINGS_CHANNEL, { action: 'status' }),
  saveXAIKey: (apiKey: string): Promise<unknown> => invokeKeyAction('save', apiKey),
  removeXAIKey: (): Promise<unknown> => ipcRenderer.invoke(XAI_SETTINGS_CHANNEL, { action: 'remove' }),
  testXAIKey: (apiKey: string): Promise<unknown> => invokeKeyAction('test', apiKey),
  restartStudio: (): Promise<unknown> => ipcRenderer.invoke(XAI_SETTINGS_CHANNEL, { action: 'restart' }),
  getMCPConfig: (): Promise<unknown> => ipcRenderer.invoke(MCP_CONFIG_CHANNEL, { action: 'info' }),
});
