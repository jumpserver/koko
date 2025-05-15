<template>
  <div id="terminal-container" class="w-full h-full"></div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { Terminal } from '@xterm/xterm';
import { onMounted, ref, watch } from 'vue';
import { useWebSocket } from '@vueuse/core';
import { generateWsURL } from '@/hooks/helper';
import { sendEventToLuna, formatMessage } from '@/utils';
import { useTerminalInstance } from '@/hooks/useTerminalInstance';
import { useConnectionStore } from '@/store/modules/useConnection';
import { useTerminalConnection } from '@/hooks/useTerminalConnection';

import { WINDOW_MESSAGE_TYPE, FORMATTER_MESSAGE_TYPE } from '@/enum';

import type { ContentType } from '@/types/modules/connection.type';

const emits = defineEmits<{
  (e: 'update:drawer', show: boolean, title: string, contentType: ContentType, token?: string): void;
  (e: 'update:protocol', protocol: string): void;
}>();

const props = defineProps<{
  shareCode?: string;

  contentType?: ContentType;
}>();

const { t } = useI18n();
const message = useMessage();
const connectionStore = useConnectionStore();

const socket = ref<WebSocket | ''>('');
const terminal = ref<Terminal | null>(null);
const lunaId = ref<string>('');
const origin = ref<string>('');
const protocol = ref<string>('');

/**
 * @description 创建 WebSocket 连接
 * @returns {WebSocket | ''}
 */
const createSocket = (): WebSocket | '' => {
  const url = generateWsURL();

  const { ws } = useWebSocket(url, {
    protocols: ['JMS-KOKO'],
    autoReconnect: {
      retries: 5,
      delay: 3000
    }
  });

  if (ws.value) {
    return ws.value;
  }

  message.error('Failed to create WebSocket connection');
  return '';
};

/**
 * @description 接收 postMessage 消息
 */
const receivePostMessage = (): void => {
  window.addEventListener('message', (e: MessageEvent) => {
    const windowMessage = e.data;

    switch (windowMessage.name) {
      case WINDOW_MESSAGE_TYPE.CMD:
        if (typeof socket.value !== 'string') {
          const termianlId = Array.from(connectionStore.connectionStateMap.values())[0].terminalId || '';
          socket.value?.send(formatMessage(termianlId, FORMATTER_MESSAGE_TYPE.TERMINAL_DATA, windowMessage.data));
        }
        break;
      case WINDOW_MESSAGE_TYPE.PING:
        origin.value = e.origin;
        lunaId.value = windowMessage.id;
        protocol.value = windowMessage.protocol;

        emits('update:protocol', protocol.value);

        sendEventToLuna(WINDOW_MESSAGE_TYPE.PONG, '', lunaId.value, origin.value);
        break;
      case WINDOW_MESSAGE_TYPE.OPEN:
        emits('update:drawer', true, t('Settings'), 'setting');
        break;
      case WINDOW_MESSAGE_TYPE.FILE:
        emits('update:drawer', true, t('FileManager'), 'file-manager', windowMessage.token.id);
        break;
      case WINDOW_MESSAGE_TYPE.FOCUS:
        terminal.value?.focus();
        break;
      case WINDOW_MESSAGE_TYPE.CREATE_FILE_CONNECT_TOKEN:
        emits('update:drawer', true, t('FileManager'), 'file-manager', windowMessage.SFTP_Token);
        break;
    }
  });
};

onMounted(() => {
  receivePostMessage();

  socket.value = createSocket();

  if (!socket.value) {
    return;
  }

  const { initializeSocketEvent, setShareCode } = useTerminalConnection(lunaId, origin);
  const { createTerminalInstance } = useTerminalInstance(socket.value);

  const terminalContainer: HTMLElement | null = document.getElementById('terminal-container');

  if (!terminalContainer) {
    return;
  }

  const terminalInstance: Terminal = createTerminalInstance(terminalContainer);

  terminal.value = terminalInstance;
  terminalInstance.open(terminalContainer);

  if (props.shareCode) {
    setShareCode(props.shareCode);
  }

  watch(
    () => props.contentType,
    (type, oldType) => {
      if (type && type === 'file-manager' && oldType) {
        sendEventToLuna(WINDOW_MESSAGE_TYPE.CREATE_FILE_CONNECT_TOKEN, '', lunaId.value, origin.value);
      }
    }
  );
  initializeSocketEvent(terminalInstance, socket.value, t);
});
</script>

<style scoped lang="scss">
#terminal-container {
  :deep(.terminal) {
    height: 100%;
    padding: 10px 0 5px 10px;

    .xterm-viewport {
      overflow-y: unset !important;
    }
  }
}
</style>
