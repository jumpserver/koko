<template>
  <div id="terminal"></div>
</template>

<script setup lang="ts">
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { useLogger } from '@/hooks/useLogger.ts';
import { bytesHuman } from '@/utils';
import { onMounted, reactive, ref } from 'vue';
import { handleEventFromLuna, sendEventToLuna, wsIsActivated, formatMessage } from '@/components/Terminal/helper';

import TerminalManager from './helper/TerminalManager';
import WebSocketManager from './helper/WebSocketManager';

import type { Reactive, Ref } from 'vue';
import type { ITerminalProps } from '../interface';

import ZmodemBrowser, { SentryConfig, Detection, ZmodemSession } from 'nora-zmodemjs/src/zmodem_browser';

const { debug, info } = useLogger('TerminalComponent');

const props = withDefaults(defineProps<ITerminalProps>(), {
  themeName: 'Default',
  enableZmodem: false
});

const emits = defineEmits<{
  (e: 'wsData', msgType: string, msg: any): void;
  (e: 'event', event: string, data: string): void;
}>();

const lunaId = ref<string>('');
const origin = ref<string>('');
const termSelectionText = ref<string>('');

const zmodemStatus = ref<boolean>(false);

const terminal = ref<Terminal | null>(null);
const websocket = ref<WebSocket | null>(null);
const fitAddon: Reactive<FitAddon> = reactive(new FitAddon());

const lastSendTime: Ref<Date> = ref(new Date());
const zsentry: Ref<ZmodemBrowser.Sentry | null> = ref(null);

// 辅助函数

const handleSendSession = (zsession: ZmodemSession) => {
  console.log(zsession);
};
const handleReceiveSession = (zsession: ZmodemSession) => {
  console.log(zsession);
};

// 主函数：创建 Terminal
const createTerminal = () => {
  const el: HTMLElement = document.getElementById('terminal')!;

  const terminalManager: TerminalManager = new TerminalManager(el);
  terminal.value = terminalManager.term!; // 将 Terminal 对象赋值给 ref

  fitAddon.fit();
  terminal.value.focus();

  // 处理 xterm 实例上相关事件
  terminal.value.onSelectionChange(() => {
    terminalManager.handleSelectionChange(terminal.value as Terminal, termSelectionText);
  });
  terminal.value.attachCustomKeyEventHandler((event: KeyboardEvent): boolean =>
    terminalManager.handleCustomKeyEvent(event, terminal.value as Terminal, lunaId.value, origin.value)
  );

  // 处理 Terminal 挂载节点的鼠标相关事件
  el.addEventListener('mouseenter', () => terminalManager.handleMouseenter(terminal.value as Terminal), false);
  el.addEventListener(
    'contextmenu',
    async ($event: MouseEvent) => {
      const text = await terminalManager.handleConextMenu($event, termSelectionText.value);

      if (wsIsActivated(websocket.value as WebSocket)) {
        // todo))
        console.log(text);
      }
    },
    false
  );

  // 监听全局 resize
  window.addEventListener(
    'resize',
    () => {
      terminalManager.handleResize(fitAddon, terminal.value as Terminal);
    },
    false
  );

  return terminalManager;
};

// 主函数：发起连接
const connect = async () => {
  debug(props.connectURL);

  // 创建 Terminal
  const terminalManager = createTerminal();

  debug(ZmodemBrowser);

  const config: SentryConfig = {
    // 将数据写入终端。
    to_terminal: (octets: string) => {
      if (zsentry.value && !zsentry.value.get_confirmed_session()) {
        terminal.value?.write(octets); // 使用 terminal ref
      }
    },
    // 将数据通过 WebSocket 发送
    sender: (octets: Uint8Array) => {
      if (!wsIsActivated(websocket.value as WebSocket)) {
        debug('WebSocket Closed');
        return;
      }
      lastSendTime.value = new Date();
      debug(`octets: ${octets}`);
      websocket.value && websocket.value.send(new Uint8Array(octets));
    },
    // 处理 Zmodem 撤回事件
    on_retract: () => {
      info('Zmodem Retract');
    },
    // 处理检测到的 Zmodem 会话
    on_detect: (detection: Detection) => {
      const zsession: ZmodemSession = detection.confirm();
      terminal.value?.write('\r\n'); // 使用 terminal ref

      if (zsession.type === 'send') {
        handleSendSession(zsession);
      } else {
        handleReceiveSession(zsession);
      }
    }
  };

  zsentry.value = new ZmodemBrowser.Sentry(config);

  terminal.value?.onData(data => {
    if (!wsIsActivated(websocket.value as WebSocket)) return debug('WebSocket Closed');

    if (!props.enableZmodem && zmodemStatus.value) return debug('未开启 Zmodem 且当前在 Zmodem 状态，不允许输入');

    lastSendTime.value = new Date();
    debug('Term on data event');

    data = terminalManager.preprocessInput(data);
    sendEventToLuna('KEYBOARDEVENT', '', lunaId.value, origin.value);

    websocket.value && websocket.value.send(formatMessage(websocketManager.terminalId, 'TERMINAL_DATA', data));
  });

  terminal.value?.onResize(({ cols, rows }) => {
    if (!wsIsActivated(websocket.value as WebSocket)) return;

    debug('Send Term Resize');

    console.log(cols, rows);
    websocket.value &&
      websocket.value.send(
        formatMessage(websocketManager.terminalId, 'TERMINAL_RESIZE', JSON.stringify({ cols, rows }))
      );
  });

  const websocketManager = new WebSocketManager(
    props.connectURL,
    terminal.value as Terminal,
    props.enableZmodem,
    zsentry.value,
    fitAddon,
    props.shareCode
  );

  websocket.value = websocketManager.connectWs();

  window.Reconnect = () => {
    emits('event', 'reconnect', '');
  };
};

onMounted(() => {
  // 监听从 Luna 发送的事件
  window.addEventListener(
    'message',
    (e: MessageEvent) => handleEventFromLuna(e, lunaId, origin, terminal, emits),
    false
  );

  // 发起连接
  connect();
});
</script>

<style scoped></style>
