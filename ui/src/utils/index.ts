import { createDiscreteApi } from 'naive-ui';
import { TranslateFunction } from '@/views/interface';
import { Terminal } from '@xterm/xterm';

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

export const fireEvent = (e: Event) => {
  window.dispatchEvent(e);
};

export const bytesHuman = (bytes: number, precision?: any) => {
  if (!/^([-+])?|(\.\d+)(\d+(\.\d+)?|(\d+\.)|Infinity)$/.test(bytes.toString())) {
    return '-';
  }
  if (bytes === 0) return '0';
  if (typeof precision === 'undefined') precision = 1;
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB', 'BB'];
  const num = Math.floor(Math.log(bytes) / Math.log(1024));
  const value = (bytes / Math.pow(1024, Math.floor(num))).toFixed(precision);
  return `${value} ${units[num]}`;
};

export const getMinuteLabel = (item: number, t: TranslateFunction): string => {
  let minuteLabel = t('Minute');

  if (item > 1) {
    minuteLabel = t('Minutes');
  }

  return `${item} ${minuteLabel}`;
};

export const writeBufferToTerminal = (
  enableZmodem: boolean,
  zmodemStatus: boolean,
  terminal: Terminal | null,
  data: any
) => {
  if (!enableZmodem && zmodemStatus)
    return message.error('未开启 Zmodem 且当前在 Zmodem 状态, 不允许显示');

  terminal && terminal.write(new Uint8Array(data));
};
