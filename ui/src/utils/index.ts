import { createDiscreteApi } from 'naive-ui';
import { TranslateFunction } from '@/types';
import { Terminal } from '@xterm/xterm';
import { AsciiBackspace, AsciiCtrlC, AsciiCtrlZ, AsciiDel } from '@/utils/config';
import type { ILunaConfig } from '@/hooks/interface';
import { RowData } from '@/components/Drawer/components/FileManagement/index.vue';

const { message } = createDiscreteApi(['message']);

/**
 * @description 复制文本功能
 * @param {string} text
 */
export const copyTextToClipboard = async (text: string): Promise<void> => {
  try {
    // Clipboard API
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(text);
      message.info('Text copied to clipboard');
    } else {
      // Fallback 方式，兼容不支持 Clipboard API 的情况
      let transfer: HTMLTextAreaElement = document.createElement('textarea');

      document.body.appendChild(transfer);
      transfer.value = text;
      transfer.focus();
      transfer.select();

      document.execCommand('copy');
      document.body.removeChild(transfer);

      message.info('Text copied to clipboard (fallback method)');
    }
  } catch (err) {
    message.error(`Failed to copy text: ${err}`);
  }
};

/**
 * @description 触发事件
 * @param e
 */
export const fireEvent = (e: Event) => {
  window.dispatchEvent(e);
};

/**
 * @description 字节转换
 * @param bytes
 * @param precision
 */
export const bytesHuman = (bytes: number, precision?: any) => {
  const regex = /^([-+]?\d+(\.\d+)?|\.\d+|Infinity)$/;

  if (!regex.test(bytes.toString())) {
    return '-';
  }

  if (bytes === 0) return '0';
  if (typeof precision === 'undefined') precision = 1;

  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB', 'BB'];
  const num = Math.floor(Math.log(bytes) / Math.log(1024));
  const value = (bytes / Math.pow(1024, Math.floor(num))).toFixed(precision);

  return `${value} ${units[num]}`;
};

/**
 * @description 获取分钟标签
 * @param item
 * @param t
 */
export const getMinuteLabel = (item: number, t: TranslateFunction): string => {
  let minuteLabel = t('Minute');

  if (item > 1) {
    minuteLabel = t('Minutes');
  }

  return `${item} ${minuteLabel}`;
};

/**
 * @description 将缓冲区写入终端
 * @param enableZmodem
 * @param zmodemStatus
 * @param terminal
 * @param data
 */
export const writeBufferToTerminal = (
  enableZmodem: boolean,
  zmodemStatus: boolean,
  terminal: Terminal | null,
  data: any
) => {
  if (!enableZmodem && zmodemStatus) return message.error('未开启 Zmodem 且当前在 Zmodem 状态, 不允许显示');

  terminal && terminal.write(new Uint8Array(data));
};

export const preprocessInput = (data: string, config: Partial<ILunaConfig>) => {
  // 如果配置项 backspaceAsCtrlH 启用（值为 "1"），并且输入数据包含删除键的 ASCII 码 (AsciiDel，即 127)，
  // 它会将其替换为退格键的 ASCII 码 (AsciiBackspace，即 8)
  if (config.backspaceAsCtrlH === '1') {
    if (data.charCodeAt(0) === AsciiDel) {
      data = String.fromCharCode(AsciiBackspace);
    }
  }

  // 如果配置项 ctrlCAsCtrlZ 启用（值为 "1"），并且输入数据包含 Ctrl+C 的 ASCII 码 (AsciiCtrlC，即 3)，
  // 它会将其替换为 Ctrl+Z 的 ASCII 码 (AsciiCtrlZ，即 26)。
  if (config.ctrlCAsCtrlZ === '1') {
    if (data.charCodeAt(0) === AsciiCtrlC) {
      data = String.fromCharCode(AsciiCtrlZ);
    }
  }

  if (data.includes('\u001b[200~') || data.includes('\u001b[201~')) {
    return data.replace(/\u001b\[200~|\u001b\[201~/g, '');
  } else {
    return data;
  }
};

/**
 * @description 处理文件名称
 * @param row
 */
export const getFileName = (row: RowData) => {
  if (row.is_dir) {
    return 'Folder';
  }

  const lastDotIndex = row.name.lastIndexOf('.');

  return lastDotIndex !== -1 ? row.name.slice(lastDotIndex + 1) : 'Folder';
};

/**
 * @description 使用 postMessage 发送事件到父窗口。
 *
 * @param {string} name - 事件的名称。
 * @param {any} data - 要随事件发送的数据。
 * @param {string | null} [lunaId=''] - Luna 实例的 ID。
 * @param {string | null} [origin=null] - 消息的来源。
 */
export const sendEventToLuna = (name: string, data: any, lunaId: string | null = '', origin: string | null = '') => {
  if (lunaId !== null && origin !== null) {
    try {
      window.parent.postMessage({ name, id: lunaId, data }, origin);
    } catch (e) {}
  }
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
 * @description 检查 WebSocket 是否已激活。
 *
 * @param ws - WebSocket 实例。
 * @returns 如果 WebSocket 已激活则返回 true，否则返回 false。
 */
export const wsIsActivated = (ws: WebSocket | undefined) => {
  return ws ? !(ws.readyState === WebSocket.CLOSING || ws.readyState === WebSocket.CLOSED) : false;
};
