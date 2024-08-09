<template>
  <n-layout style="height: 100vh">
    <n-scrollbar trigger="hover" style="max-height: 880px">
      <div id="terminal" class="terminal-container"></div>
    </n-scrollbar>
  </n-layout>
</template>

<script setup lang="ts">
// 引入 hook
import { useI18n } from 'vue-i18n';
import { useLogger } from '@/hooks/useLogger.ts';
import { useTerminal } from '@/hooks/useTerminal.ts';
import { useWebSocket } from '@/hooks/useWebSocket.ts';
import { useTerminalStore } from '@/store/modules/terminal.ts';

// 类型声明
import type { ILunaConfig, ITerminalProps } from '@/hooks/interface';

import { Terminal } from '@xterm/xterm';
import { onMounted, onUnmounted, Ref, ref } from 'vue';
import { formatMessage, sendEventToLuna, wsIsActivated } from '@/components/Terminal/helper';

import mittBus from '@/utils/mittBus.ts';

const { t } = useI18n();
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
  (e: 'wsData', msgType: string, msg: any, terminal: Terminal, setting: any): void;
}>();

const lunaId = ref('');
const origin = ref('');
const terminalId = ref('');
const currentUser = ref('');

const zmodemStatus = ref(false);

const lastSendTime: Ref<Date> = ref(new Date());
const lunaConfig: Ref<ILunaConfig> = ref({});

// 使用 hook
const { createTerminal, setTerminalTheme, initTerminalEvent } = useTerminal(
  terminalId,
  'common',
  zmodemStatus,
  props.enableZmodem,
  lastSendTime,
  emits
);

const { createWebSocket } = useWebSocket(
  terminalId,
  props.enableZmodem,
  zmodemStatus,
  props.shareCode,
  currentUser,
  emits,
  t
);

const sendDataFromWindow = (
  data: any,
  ws: Ref<WebSocket>,
  send: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean
): void => {
  if (!wsIsActivated(ws.value)) return debug('WebSocket Disconnected');

  if (props.enableZmodem && !zmodemStatus.value) {
    send(formatMessage(terminalId.value, 'TERMINAL_DATA', data));
    debug('Send Data From Window');
  }
};

const handleCustomWindowEvent = (
  terminal: Terminal,
  ws: Ref<WebSocket>,
  send: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean
) => {
  window.addEventListener(
    'message',
    (e: MessageEvent) => {
      const message = e.data;

      switch (message.name) {
        case 'PING': {
          if (lunaId.value != null) return;

          lunaId.value = message.id;
          origin.value = e.origin;

          sendEventToLuna('PONG', '', lunaId.value, origin.value);
          break;
        }
        case 'CMD': {
          sendDataFromWindow(message.data, ws, send);
          break;
        }
        case 'FOCUS': {
          terminal.focus();
          break;
        }
        case 'OPEN': {
          emits('event', 'open', '');
          break;
        }
      }
    },
    false
  );

  window.SendTerminalData = data => {
    sendDataFromWindow(data, ws, send);
  };

  window.Reconnect = () => {
    emits('event', 'reconnect', '');
  };
};

const sendWsMessage = (
  type: string,
  data: any,
  ws: Ref<WebSocket>,
  send: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean
) => {
  if (wsIsActivated(ws.value)) {
    return send(formatMessage(terminalId.value, type, JSON.stringify(data)));
  }
};

const handleSendSesson = (zsession: any) => {};

onMounted(() => {
  const theme = props.themeName;
  const el: HTMLElement = document.getElementById('terminal')!;

  const terminalStore = useTerminalStore();

  lunaConfig.value = terminalStore.getConfig;

  // 创建 Terminal
  const { terminal, fitAddon } = createTerminal(el, lunaConfig.value);

  // 创建 WebSocket
  const { send, ws } = createWebSocket(terminal, lastSendTime, fitAddon);

  // 初始化 el 与 Terminal 相关事件
  initTerminalEvent(ws.value, el, terminal, lunaConfig.value);

  // 事件监听相关逻辑
  handleCustomWindowEvent(terminal, ws, send);

  // 设置主题
  setTerminalTheme(theme, terminal);

  // 修改主题
  mittBus.on('set-theme', ({ themeName }) => {
    setTerminalTheme(themeName as string, terminal);
  });

  mittBus.on('sync-theme', ({ type, data }) => {
    sendWsMessage(type, data, ws, send);
  });

  mittBus.on('share-user', ({ type, query }) => {
    sendWsMessage(type, { query }, ws, send);
  });

  mittBus.on('create-share-url', ({ type, sessionId, shareLinkRequest }) => {
    const origin = window.location.origin;

    sendWsMessage(
      type,
      {
        origin,
        session: sessionId,
        users: shareLinkRequest.users,
        expired_time: shareLinkRequest.expiredTime,
        action_permission: shareLinkRequest.actionPerm
      },
      ws,
      send
    );
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
