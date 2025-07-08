import type { Terminal } from '@xterm/xterm';

import { createDiscreteApi } from 'naive-ui';

import type { TranslateFunction } from '@/types';
import type { ILunaConfig } from '@/types/modules/config.type';
import type { RowData } from '@/components/Drawer/components/FileManagement/index.vue';

import { AsciiBackspace, AsciiCtrlC, AsciiCtrlZ, AsciiDel } from '@/utils/config';

const { message } = createDiscreteApi(['message']);

/**
 * @description 获取分钟标签
 * @param item
 * @param t
 */
export function getMinuteLabel(item: number, t: TranslateFunction): string {
  let minuteLabel = t('Minute');

  if (item > 1) {
    minuteLabel = t('Minutes');
  }

  return `${item} ${minuteLabel}`;
}

/**
 * @description 将缓冲区写入终端
 * @param enableZmodem
 * @param zmodemStatus
 * @param terminal
 * @param data
 */
export function writeBufferToTerminal(
  enableZmodem: boolean,
  zmodemStatus: boolean,
  terminal: Terminal | null,
  data: any
) {
  if (!enableZmodem && zmodemStatus) return message.error('未开启 Zmodem 且当前在 Zmodem 状态, 不允许显示');
  if (!terminal) return;
  terminal.write(new Uint8Array(data));
}

export function preprocessInput(data: string, config: Partial<ILunaConfig>) {
  // 如果配置项 backspaceAsCtrlH 启用（值为 "1"），并且输入数据包含删除键的 ASCII 码 (AsciiDel，即 127)，
  // 它会将其替换为退格键的 ASCII 码 (AsciiBackspace，即 8)
  if (config.backspaceAsCtrlH === '1') {
    if (data.charCodeAt(0) === AsciiDel) {
      data = String.fromCharCode(AsciiBackspace);
    }
  }

  if (config.ctrlCAsCtrlZ === '1') {
    if (data.charCodeAt(0) === AsciiCtrlC) {
      data = String.fromCharCode(AsciiCtrlZ);
    }
  }

  // 使用字符串替换方法避免在正则表达式中使用控制字符
  const escSeq200 = '\u001B[200~';
  const escSeq201 = '\u001B[201~';

  if (data.includes(escSeq200) || data.includes(escSeq201)) {
    return data.replace(escSeq200, '').replace(escSeq201, '');
  }

  return data;
}

/**
 * @description 处理文件名称
 * @param row
 */
export function getFileName(row: RowData) {
  if (row.is_dir) {
    return 'Folder';
  }

  const lastDotIndex = row.name.lastIndexOf('.');

  return lastDotIndex !== -1 ? row.name.slice(lastDotIndex + 1) : 'File';
}

/**
 * @description 使用 postMessage 发送事件到父窗口。
 *
 * @param {string} name - 事件的名称。
 * @param {any} data - 要随事件发送的数据。
 * @param {string | null} [lunaId] - Luna 实例的 ID。
 * @param {string | null} [origin] - 消息的来源。
 */
export function sendEventToLuna(name: string, data: any, lunaId: string | null = '', origin: string | null = '') {
  if (lunaId !== null && origin !== null) {
    try {
      window.parent.postMessage({ name, id: lunaId, data }, origin);
    } catch (e) {
      console.error(e);
    }
  }
}

/**
 * @description 格式化消息为 JSON 字符串。
 *
 * @param id - 消息的 ID。
 * @param type - 消息的类型。
 * @param data - 消息的数据。
 * @returns 格式化的 JSON 字符串。
 */
export function formatMessage(id: string, type: string, data: any) {
  return JSON.stringify({
    id,
    type,
    data,
  });
}
