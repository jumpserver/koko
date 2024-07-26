import { Ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { useLogger } from '@/hooks/useLogger.ts';

const { info, debug } = useLogger('HelperFunctions');

interface LunaEventMessage {
  name: string;
  id?: string;
  data?: any;
}

export const sendEventToLuna = (name: string, data: any, lunaId: string | null = '', origin: string | null = '') => {
  if (lunaId !== null && origin !== null) {
    window.parent.postMessage({ name, id: lunaId, data }, origin);
  }
};

export const handleEventFromLuna = (
  e: MessageEvent,
  lunaId: Ref<string | null>,
  origin: Ref<string | null>,
  terminal: Ref<Terminal | null>,
  emits: (event: 'event', eventName: string, data: string) => void
) => {
  const msg: LunaEventMessage = e.data;

  info('Received post message:', msg);

  switch (msg.name) {
    case 'PING':
      if (lunaId.value != null) return;

      lunaId.value = msg.id || null;
      origin.value = e.origin;
      sendEventToLuna('PONG', '', lunaId.value, origin.value);
      break;
    case 'CMD':
      sendEventToLuna('CMD', msg.data || '', lunaId.value, origin.value);
      break;
    case 'FOCUS':
      terminal.value?.focus();
      break;
    case 'OPEN':
      emits('event', 'open', '');
      break;
  }
};

export const wsIsActivated = (ws: WebSocket) => {
  return ws ? !(ws.readyState === WebSocket.CLOSING || ws.readyState === WebSocket.CLOSED) : false;
};

export const handleError = (e: Event) => {
  info(e);
};

export const writeBufferToTerminal = (enableZmodem: boolean, zmodemStatus: any, data: any, terminal: Terminal) => {
  if (!enableZmodem && zmodemStatus) return debug('未开启 Zmodem 且当前在 Zmodem状态, 不允许显示');

  terminal.write(new Uint8Array(data));
};

export const formatMessage = (id: string, type: string, data: any) => {
  return JSON.stringify({
    id,
    type,
    data
  });
};

export const updateIcon = (setting: any) => {
  const faviconURL = setting['LOGO_URLS']?.favicon;
  let link = document.querySelector("link[rel*='icon']") as HTMLLinkElement;
  if (!link) {
    link = document.createElement('link') as HTMLLinkElement;
    link.type = 'image/x-icon';
    link.rel = 'shortcut icon';
    document.getElementsByTagName('head')[0].appendChild(link);
  }
  if (faviconURL) {
    link.href = faviconURL;
  }
};
