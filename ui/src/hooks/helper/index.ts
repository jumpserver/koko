import type { Terminal } from '@xterm/xterm';

// 引入 API
import { useRoute } from 'vue-router';
import { createDiscreteApi } from 'naive-ui';
import { readText } from 'clipboard-polyfill';

import type { ILunaConfig } from '@/types/modules/config.type';

import { formatMessage } from '@/utils';
import { BASE_WS_URL } from '@/utils/config';

const { message } = createDiscreteApi(['message']);

/**
 * 右键复制文本
 *
 * @param e
 * @param config
 * @param socket
 * @param terminalId
 * @param termSelectionText
 */
export async function handleContextMenu(
  e: MouseEvent,
  config: ILunaConfig,
  socket: WebSocket,
  terminalId: string,
  termSelectionText: string
) {
  if (e.ctrlKey || config.quickPaste !== '1') return;

  let text: string = '';

  try {
    text = await readText();
  } catch {
    if (termSelectionText !== '') text = termSelectionText;
  }
  e.preventDefault();

  socket.send(formatMessage(terminalId, 'TERMINAL_DATA', text));
}

/**
 * 生成 Socket url
 */
export function generateWsURL() {
  const route = useRoute();

  const routeName = route.name;
  const urlParams = new URLSearchParams(window.location.search.slice(1));

  let connectURL;

  switch (routeName) {
    case 'Token': {
      const params = route.params;
      const requireParams = new URLSearchParams();

      requireParams.append('type', 'token');
      requireParams.append('target_id', params.id ? params.id.toString() : '');

      connectURL = `${BASE_WS_URL}/koko/ws/token/?${requireParams.toString()}`;
      break;
    }
    case 'TokenParams': {
      connectURL = urlParams ? `${BASE_WS_URL}/koko/ws/token/?${urlParams.toString()}` : '';
      break;
    }
    case 'kubernetes': {
      connectURL = `${BASE_WS_URL}/koko/ws/terminal/?token=${route.query.token}&type=k8s`;
      break;
    }
    case 'Share': {
      const id = route.params.id as string;
      const requireParams = new URLSearchParams();

      requireParams.append('type', 'share');
      requireParams.append('target_id', id);

      connectURL = `${BASE_WS_URL}/koko/ws/terminal/?${requireParams.toString()}`;
      break;
    }
    case 'Monitor': {
      const id = route.params.id as string;
      const requireParams = new URLSearchParams();

      requireParams.append('type', 'monitor');
      requireParams.append('target_id', id);

      connectURL = `${BASE_WS_URL}/koko/ws/terminal/?${requireParams.toString()}`;
      break;
    }
    default: {
      connectURL = urlParams ? `${BASE_WS_URL}/koko/ws/terminal/?${urlParams.toString()}` : '';
    }
  }

  if (!connectURL) {
    message.error('Unable to generate WebSocket URL, missing parameters.');
  }

  return connectURL;
}

/**
 * @description 将 Base64 转化为字节数组
 */
export function base64ToUint8Array(base64: string): Uint8Array {
  // 转为原始的二进制字符串（binaryString）。
  const binaryString = atob(base64);
  const len = binaryString.length;

  const bytes = new Uint8Array(len);
  for (let i = 0; i < len; i++) {
    bytes[i] = binaryString.charCodeAt(i);
  }
  return bytes;
}

/**
 * @description 更新网页图标。
 *
 * @param {any} setting - 包含 LOGO_URLS 配置的设置对象。
 */
export function updateIcon(setting: any) {
  const faviconURL = setting.INTERFACE.favicon;

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
}

export const getXTerminalLineContent = (index: number, terminal: Terminal) => {
  const buffer = terminal.buffer.active;

  if (!buffer) return '';

  const result: string[] = [];
  const bufferLineCount = buffer.length;

  let startLine = bufferLineCount;

  while (true) {
    if (result.length > index || startLine <= 0) {
      console.warn(`Line ${startLine} is empty or result.length > ${result.length}`);
      break;
    }
    const line = buffer.getLine(startLine);
    const stripLine = line?.translateToString(true);
    startLine--;
    if (!stripLine) {
      console.warn(`Line ${startLine} is empty or undefined`);
      continue;
    }
    result.unshift(stripLine);
  }
  return result.join('\n');
};
