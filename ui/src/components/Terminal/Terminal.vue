<template>
  <n-layout style="height: 100vh">
    <n-scrollbar trigger="hover" style="max-height: 880px">
      <div id="terminal" class="terminal-container"></div>
    </n-scrollbar>
  </n-layout>
</template>

<script setup lang="ts">
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { useLogger } from '@/hooks/useLogger.ts';
import { useSentry } from '@/hooks/useZsentry.ts';
import { useTerminal } from '@/hooks/useTerminal.ts';
import { useWebSocket } from '@/hooks/useWebSocket.ts';
import { useTerminalStore } from '@/store/modules/terminal.ts';
import { onMounted, onUnmounted, Ref, ref } from 'vue';
import { formatMessage, handleEventFromLuna, wsIsActivated } from '@/components/Terminal/helper';

import type { ILunaConfig, ITerminalProps } from '@/hooks/interface';

import mittBus from '@/utils/mittBus.ts';
import ZmodemBrowser from 'nora-zmodemjs/src/zmodem_browser';

const { debug } = useLogger('TerminalComponent');

// prop 参数
const props = withDefaults(defineProps<ITerminalProps>(), {
  themeName: 'Default',
  enableZmodem: false
});

// emit 事件
const emits = defineEmits<{
  (e: 'event', event: string, data: string): void;
  (e: 'background-color', backgroundColor: string): void;
  (e: 'wsData', msgType: string, msg: any, terminal: Terminal): void;
}>();

const lunaId = ref('');
const origin = ref('');
const terminalId = ref('');
const currentUser = ref('');

const zmodemStatus = ref(false);

const lastSendTime: Ref<Date> = ref(new Date());
const lunaConfig: Ref<ILunaConfig> = ref({});

let term: Terminal;
let socket: WebSocket;
let fitAddon: FitAddon;
let sentryRef: Ref<ZmodemBrowser.Sentry | null> = ref(null);

// 使用 hook
const { createSentry } = useSentry(lastSendTime);
const { createTerminal, setTerminalTheme, initTerminalEvent } = useTerminal(
  terminalId,
  zmodemStatus,
  props.enableZmodem,
  lastSendTime,
  emits
);

const { createWebSocket } = useWebSocket(
  props.enableZmodem,
  zmodemStatus,
  sentryRef,
  fitAddon,
  props.shareCode,
  currentUser,
  emits
);

const handleCustomWindowEvent = (terminal: Terminal) => {
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

const sendWsMessage = (type: string, data: any) => {
  if (wsIsActivated(socket))
    return socket.send(formatMessage(terminalId.value, type, JSON.stringify(data)));
};
const sendDataFromWindow = (data: any): void => {
  if (!wsIsActivated(socket)) return debug('WebSocket Disconnected');

  if (props.enableZmodem && !zmodemStatus.value) {
    socket.send(formatMessage(terminalId.value, 'TERMINAL_DATA', data));
    debug('Send Data From Window');
  }
};

onMounted(() => {
  const theme = props.themeName;
  const el: HTMLElement = document.getElementById('terminal')!;

  const terminalStore = useTerminalStore();

  lunaConfig.value = terminalStore.getConfig;

  // 创建 Terminal
  const { terminal, fitAddon: createdFitAddon } = createTerminal(el, lunaConfig.value);

  fitAddon = createdFitAddon;

  // 创建 WebSocket
  socket = createWebSocket(terminal, lastSendTime, terminalId);

  // 创建 Sentry
  sentryRef.value = createSentry(socket, terminal, sentryRef);

  // 初始化 el 与 Terminal 相关事件
  initTerminalEvent(socket, el, terminal, lunaConfig.value);

  handleCustomWindowEvent(terminal);

  // 设置主题
  setTerminalTheme(theme, terminal);

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
