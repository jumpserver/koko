import { Ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { useLogger } from '@/hooks/useLogger.ts';

const { info } = useLogger('HelperFunctions');

interface LunaEventMessage {
  name: string;
  id?: string;
  data?: any;
}

/**
 * @description 使用 postMessage 发送事件到父窗口。
 *
 * @param {string} name - 事件的名称。
 * @param {any} data - 要随事件发送的数据。
 * @param {string | null} [lunaId=''] - Luna 实例的 ID。
 * @param {string | null} [origin=null] - 消息的来源。
 */
export const sendEventToLuna = (
  name: string,
  data: any,
  lunaId: string | null = '',
  origin: string | null = null
) => {
  if (lunaId !== null && origin !== null) {
    window.parent.postMessage({ name, id: lunaId, data }, origin);
  }
};

/**
 * @description 处理从父窗口接收到的事件。
 *
 * @param {MessageEvent} e - 接收到的消息事件。
 * @param {(event: 'event', eventName: string, data: string) => void} emits - Vue 的 emit 函数。
 * @param {Ref<string | null>} lunaId - Luna 实例的 ID。
 * @param {Ref<string | null>} origin - 消息的来源。
 * @param {Terminal} terminal - xterm.js 的终端实例。
 * @param {(data: any) => void} sendDataFromWindow - 从窗口发送数据的函数。
 */
export const handleEventFromLuna = (
  e: MessageEvent,
  emits: (event: 'event', eventName: string, data: string) => void,
  lunaId: Ref<string | null>,
  origin: Ref<string | null>,
  terminal: Terminal,
  sendDataFromWindow: (data: any) => void
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
      sendDataFromWindow(msg.data);
      break;
    case 'FOCUS':
      terminal.focus();
      break;
    case 'OPEN':
      emits('event', 'open', '');
      break;
  }
};

/**
 * @description 检查 WebSocket 是否已激活。
 *
 * @param ws - WebSocket 实例。
 * @returns 如果 WebSocket 已激活则返回 true，否则返回 false。
 */
export const wsIsActivated = (ws: WebSocket) => {
  return ws ? !(ws.readyState === WebSocket.CLOSING || ws.readyState === WebSocket.CLOSED) : false;
};

/**
 * @description 处理错误事件。
 *
 * @param {Event} e - 错误事件。
 */
export const handleError = (e: any) => {
  info(e);
};

/**
 * @description 格式化消息为 JSON 字符串。
 *
 * @param id - 消息的 ID。
 * @param type - 消息的类型。
 * @param data - 消息的数据。
 * @returns 格式化的 JSON 字符串。
 */
export const formatMessage = (id: string, type: string, data: any) => {
  return JSON.stringify({
    id,
    type,
    data
  });
};

/**
 * @description 更新网页图标。
 *
 * @param {any} setting - 包含 LOGO_URLS 配置的设置对象。
 */
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
