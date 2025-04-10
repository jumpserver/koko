import { Ref } from 'vue';
import type { ILunaConfig } from '@/hooks/interface';

import { Terminal } from '@xterm/xterm';
import { useDebounceFn } from '@vueuse/core';
import {
  formatMessage,
  handleError,
  sendEventToLuna
} from '@/components/TerminalComponent/helper';

// 引入 Store
import { useTreeStore } from '@/store/modules/tree.ts';
import { useTerminalStore } from '@/store/modules/terminal.ts';

// 引入 API
import { storeToRefs } from 'pinia';
import { useRoute } from 'vue-router';
import { createDiscreteApi } from 'naive-ui';
import { readText } from 'clipboard-polyfill';
import { fireEvent, preprocessInput } from '@/utils';

import mittBus from '@/utils/mittBus.ts';
import * as clipboard from 'clipboard-polyfill';
import { BASE_WS_URL, MaxTimeout } from '@/config';

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
export const handleContextMenu = async (
  e: MouseEvent,
  config: ILunaConfig,
  socket: WebSocket,
  terminalId: string,
  termSelectionText: string
) => {
  if (e.ctrlKey || config.quickPaste !== '1') return;

  let text: string = '';

  try {
    text = await readText();
  } catch (e) {
    if (termSelectionText !== '') text = termSelectionText;
  }
  e.preventDefault();

  socket.send(formatMessage(terminalId, 'TERMINAL_DATA', text));
};

/**
 * CustomTerminal Resize 事件处理
 *
 * @param cols
 * @param rows
 * @param type
 * @param terminalId
 * @param socket
 */
export const handleTerminalResize = (
  cols: number,
  rows: number,
  type: string,
  terminalId: string,
  socket: WebSocket
) => {
  let data;

  const treeStore = useTreeStore();
  const { currentNode } = storeToRefs(treeStore);

  const eventType = type === 'k8s' ? 'TERMINAL_K8S_RESIZE' : 'TERMINAL_RESIZE';
  const resizeData = JSON.stringify({ cols, rows });

  data = resizeData;

  if (type === 'k8s' && currentNode.value.children) {
    const currentItem = currentNode.value.children[0];

    data = {
      k8s_id: currentItem.k8s_id,
      namespace: currentItem.namespace,
      pod: currentItem.pod,
      container: currentItem.container,
      type: eventType,
      id: terminalId,
      resizeData
    };
  }

  socket.send(formatMessage(terminalId, eventType, data));
};

// 将防抖函数移到外部，确保只创建一次
const debouncedSwitchTab = useDebounceFn(
  (lunaId: string, origin: string, key: string) => {
    console.log('key');
    switch (key) {
      case 'ArrowRight':
        sendEventToLuna('KEYEVENT', 'alt+shift+right', lunaId, origin);
        break;
      case 'ArrowLeft':
        sendEventToLuna('KEYEVENT', 'alt+shift+left', lunaId, origin);
        break;
    }
  },
  500
);

/**
 * 针对特定的键盘组合进行操作
 *
 * @param e
 * @param terminal
 * @param lunaId
 * @param origin
 */
export const handleCustomKey = (
  e: KeyboardEvent,
  terminal: Terminal,
  lunaId: string,
  origin: string
): boolean => {
  if (
    e.altKey &&
    e.shiftKey &&
    (e.key === 'ArrowRight' || e.key === 'ArrowLeft')
  ) {
    if (lunaId && origin) {
      debouncedSwitchTab(lunaId, origin, e.key);
    } else {
      mittBus.emit(
        e.key === 'ArrowRight' ? 'alt-shift-right' : 'alt-shift-left'
      );
    }
    return false;
  }

  if (e.ctrlKey && e.key === 'c' && terminal.hasSelection()) {
    return false;
  }

  return !(e.ctrlKey && e.key === 'v');
};

/**
 *左键选中
 *
 * @param terminal
 * @param termSelectionText
 */
export const handleTerminalSelection = async (
  terminal: Terminal,
  termSelectionText: Ref<string>
) => {
  termSelectionText.value = terminal.getSelection().trim();

  if (termSelectionText.value !== '') {
    clipboard
      .writeText(termSelectionText.value)
      .then(() => {})
      .catch(e => {
        message.error(`Copy Error for ${e}`);
      });
  } else {
    // message.warning('Please select the text before copying');
  }
};

/**
 * 处理 CustomTerminal 的输入事件
 *
 * @param data
 * @param type
 * @param terminalId
 * @param config
 * @param socket
 */
export const handleTerminalOnData = (
  data: string,
  type: string,
  terminalId: string,
  config: ILunaConfig,
  socket: WebSocket
) => {
  const terminalStore = useTerminalStore();
  const { enableZmodem, zmodemStatus } = storeToRefs(terminalStore);

  // 如果未开启 Zmodem 且当前在 Zmodem 状态，不允许输入
  if (!enableZmodem.value && zmodemStatus.value) {
    return message.warning('未开启 Zmodem 且当前在 Zmodem 状态，不允许输入');
  }

  data = preprocessInput(data, config);
  const eventType = type === 'k8s' ? 'TERMINAL_K8S_DATA' : 'TERMINAL_DATA';

  // 如果类型是 k8s，处理 k8s 的逻辑
  if (type === 'k8s') {
    const treeStore = useTreeStore();
    const { currentNode } = storeToRefs(treeStore);
    const node = currentNode.value;

    // 获取默认的消息体
    const messageData = {
      data: data,
      id: terminalId,
      type: eventType,
      pod: node.pod || '',
      k8s_id: node.k8s_id,
      namespace: node.namespace || '',
      container: node.container || ''
    };

    // 如果有子节点但不是父节点，取第一个子节点的信息
    if (node.children && node.children.length > 0) {
      const currentItem = node.children[0];
      Object.assign(messageData, {
        pod: currentItem.pod,
        k8s_id: currentItem.k8s_id,
        namespace: currentItem.namespace,
        container: currentItem.container
      });
    }

    // 发送消息
    return socket.send(JSON.stringify(messageData));
  }

  // 处理非 k8s 的情况
  sendEventToLuna('KEYBOARDEVENT', '');
  socket.send(formatMessage(terminalId, eventType, data));
};

/**
 * Socket 打开时的回调
 *
 * @param socket
 * @param lastSendTime
 * @param pingInterval
 * @param lastReceiveTime
 * @param terminalId
 */
export const onWebsocketOpen = (
  socket: WebSocket,
  lastSendTime: Date,
  terminalId: string,
  pingInterval: Ref<number | null>,
  lastReceiveTime: Ref<Date>
) => {
  socket.binaryType = 'arraybuffer';
  sendEventToLuna('CONNECTED', '');

  if (pingInterval.value) clearInterval(pingInterval.value);

  pingInterval.value = setInterval(() => {
    if (
      socket.CLOSED === socket.readyState ||
      socket.CLOSING === socket.readyState
    ) {
      return clearInterval(pingInterval.value!);
    }

    let currentDate: Date = new Date();

    if (lastReceiveTime.value.getTime() - currentDate.getTime() > MaxTimeout) {
    }

    let pingTimeout: number = currentDate.getTime() - lastSendTime.getTime();

    if (pingTimeout < 0) return;

    socket.send(formatMessage(terminalId, 'PING', ''));
  }, 25 * 1000);
};

/**
 * 生成 Socket url
 */
export const generateWsURL = () => {
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

      connectURL = BASE_WS_URL + '/koko/ws/token/?' + requireParams.toString();
      break;
    }
    case 'TokenParams': {
      connectURL = urlParams
        ? `${BASE_WS_URL}/koko/ws/token/?${urlParams.toString()}`
        : '';
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

      connectURL =
        BASE_WS_URL + '/koko/ws/terminal/?' + requireParams.toString();
      break;
    }
    case 'Monitor': {
      const id = route.params.id as string;
      const requireParams = new URLSearchParams();

      requireParams.append('type', 'monitor');
      requireParams.append('target_id', id);

      connectURL =
        BASE_WS_URL + '/koko/ws/terminal/?' + requireParams.toString();
      break;
    }
    default: {
      connectURL = urlParams
        ? `${BASE_WS_URL}/koko/ws/terminal/?${urlParams.toString()}`
        : '';
    }
  }

  if (!connectURL) {
    message.error('Unable to generate WebSocket URL, missing parameters.');
  }

  return connectURL;
};

/**
 * Socket 出错或断开连接的回调
 *
 * @param event
 * @param terminal
 * @param type
 */
export const onWebsocketWrong = (
  event: Event,
  type: string,
  terminal?: Terminal
) => {
  switch (type) {
    case 'error': {
      terminal
        ? terminal.write('\x1b[31mConnection Websocket Error\x1b[0m' + '\r\n')
        : '';
      break;
    }
    case 'disconnected': {
      terminal
        ? terminal.write('\x1b[31mConnection Websocket Closed\x1b[0m')
        : '';
      break;
    }
  }

  fireEvent(new Event('CLOSE', {}));
  handleError(event);
};

/**
 * @description 将 Base64 转化为字节数组
 * @param base64
 */
export const base64ToUint8Array = (base64: string): Uint8Array => {
  // 转为原始的二进制字符串（binaryString）。
  const binaryString = atob(base64);
  const len = binaryString.length;

  const bytes = new Uint8Array(len);
  for (let i = 0; i < len; i++) {
    bytes[i] = binaryString.charCodeAt(i);
  }
  return bytes;
};
