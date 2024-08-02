<template>
  <n-layout style="height: 100vh">
    <n-scrollbar trigger="hover" style="max-height: 880px">
      <div id="terminal" class="terminal-container"></div>
    </n-scrollbar>
  </n-layout>
</template>

<script setup lang="ts">
import { Terminal } from '@xterm/xterm';
import { useLogger } from '@/hooks/useLogger.ts';
import { useTerminal } from '@/hooks/useTerminal.ts';
import { useWebSocket } from '@/hooks/useWebSocket.ts';
import { onMounted, onUnmounted, Ref, ref } from 'vue';

import type { ILunaConfig } from '@/hooks/interface';
import type { ITerminalProps } from '@/hooks/interface';

import { handleEventFromLuna, wsIsActivated, formatMessage } from '@/components/Terminal/helper';
import ZmodemBrowser, {
  SentryConfig,
  Detection,
  ZmodemSession
} from 'nora-zmodemjs/src/zmodem_browser';
import mittBus from '@/utils/mittBus.ts';
import { useZsentry } from '@/hooks/useZsentry.ts';

const { debug, info } = useLogger('TerminalComponent');

const props = withDefaults(defineProps<ITerminalProps>(), {
  themeName: 'Default',
  enableZmodem: false
});

const emits = defineEmits<{
  (e: 'wsData', msgType: string, msg: any, terminal: Terminal, setting: any): void;
  (e: 'event', event: string, data: string): void;
  (e: 'background-color', backgroundColor: string): void;
}>();

const {
  createTerminal,
  setTerminalTheme,
  handleTerminalOnResize,
  handleTerminalOnData,
  initTerminalEvent
} = useTerminal();
const { createZsentry } = useZsentry();

import { useTerminalStore } from '@/store/modules/terminal.ts';
import { FitAddon } from '@xterm/addon-fit';

const lunaId = ref<string>('');
const origin = ref<string>('');
const terminalId = ref<string>('');
const currentUser = ref<string>('');

const setting = ref<any>(null);
const zmodemStatus = ref<boolean>(false);
const lastSendTime: Ref<Date> = ref(new Date());

let ws: WebSocket;
let zsentryRef: Ref<ZmodemBrowser.Sentry | null> = ref(null);
let gFitAddon: Ref<FitAddon | null> = ref(null);
let term: Terminal;

const lunaConfig: Ref<ILunaConfig> = ref({});

const { createWebSocket } = useWebSocket(
  props.enableZmodem,
  zmodemStatus,
  zsentryRef,
  gFitAddon.value as FitAddon,
  props.shareCode,
  currentUser,
  setting,
  emits
);

// let zmodeSession: ZmodemSession;
// const zmodeDialogVisible = ref(false);

const handleCunstomWindowEvent = (terminal: Terminal) => {
  window.addEventListener(
    'message',
    (e: MessageEvent) =>
      handleEventFromLuna(e, emits, lunaId, origin, terminal, sendDataFromWindow),
    false
  );

  window.SendTerminalData = sendDataFromWindow;

  window.Reconnect = () => {
    emits('event', 'reconnect', '');
  };
};
const handleTerminalEvent = (terminal: Terminal) => {
  terminal.onData(data =>
    handleTerminalOnData(
      ws,
      data,
      terminalId.value,
      lunaConfig,
      props.enableZmodem,
      zmodemStatus.value,
      lastSendTime
    )
  );

  terminal.onResize(({ cols, rows }) => handleTerminalOnResize(ws, cols, rows, terminalId.value));
};

const sendWsMessage = (type: string, data: any) => {
  if (wsIsActivated(ws))
    return ws.send(formatMessage(terminalId.value, type, JSON.stringify(data)));
};
const sendDataFromWindow = (data: any): void => {
  if (!wsIsActivated(ws)) return debug('WebSocket Disconnected');

  if (props.enableZmodem && !zmodemStatus.value) {
    ws.send(formatMessage(terminalId.value, 'TERMINAL_DATA', data));
    debug('Send Data From Window');
  }
};

// 处理需要 ws 的 Terminal 事件
// handleTerminalEvent(terminal);

onMounted(() => {
  const el: HTMLElement = document.getElementById('terminal')!;

  const terminalStore = useTerminalStore();

  lunaConfig.value = terminalStore.getConfig;

  // 创建 Terminal
  const { terminal, fitAddon } = createTerminal(el, lunaConfig.value);

  gFitAddon.value = fitAddon;

  // 初始化 el 与 Terminal 相关事件
  initTerminalEvent(ws, el, terminal, lunaConfig.value);

  // 创建 Zsentry
  zsentryRef.value = createZsentry(
    ws,
    terminal,
    zsentryRef.value as ZmodemBrowser.Sentry,
    lastSendTime
  );

  // 创建 WebSocket
  ws = createWebSocket(terminal, lastSendTime, terminalId);

  handleCunstomWindowEvent(terminal);

  // 设置主题
  // setTerminalTheme(props.themeName, term, emits);

  // 修改主题
  mittBus.on('set-theme', ({ themeName }) => {
    setTerminalTheme(themeName as string, term);
  });

  mittBus.on('sync-theme', ({ type, data }) => {
    sendWsMessage(type, data);
  });

  mittBus.on('share-user', ({ type, query }) => {
    sendWsMessage(type, { query });
  });

  mittBus.on('create-share-url', ({ type, sessionId, shareLinkRequest }) => {
    const origin = window.location.origin;

    sendWsMessage(type, {
      origin,
      session: sessionId,
      users: shareLinkRequest.users,
      expired_time: shareLinkRequest.expiredTime,
      action_permission: shareLinkRequest.actionPerm
    });
  });
});

onUnmounted(() => {
  mittBus.off('set-theme');
  mittBus.off('sync-theme');
  mittBus.off('share-user');
  mittBus.off('create-share-url');
});
</script>

<style scoped lang="scss">
.terminal-container {
  height: calc(100% - 10px);
  overflow: hidden;

  :deep(.xterm-viewport) {
    overflow: hidden;
  }

  :deep(.xterm-screen) {
    height: 878px !important;

    .xterm-rows {
      //padding: 10px 0 0 10px;
    }
  }
}
</style>
