<template>
  <div id="terminal-container" class="w-full h-full"></div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { onMounted, ref } from 'vue';
import { useMessage } from 'naive-ui';
import { Terminal } from '@xterm/xterm';
import { useWebSocket } from '@vueuse/core';
import { generateWsURL } from '@/hooks/helper';
import { useTerminalInstance } from '@/hooks/useTerminalInstance';
import { useTerminalConnection } from '@/hooks/useTerminalConnection';
import { sendEventToLuna } from '@/utils';

import { WINDOW_MESSAGE_TYPE } from '@/enum';

import type { ContentType } from '@/types/modules/connection.type';

const emits = defineEmits<{
  (e: 'update:drawer', show: boolean, title: string, contentType: ContentType, token?: string): void;
}>();

const props = defineProps<{
  shareCode?: string;
}>();

const { t } = useI18n();
const message = useMessage();

const socket = ref<WebSocket | ''>('');
const lunaId = ref<string>('');
const origin = ref<string>('');

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

const receivePostMessage = (): void => {
  window.addEventListener('message', (e: MessageEvent) => {
    const windowMessage = e.data;

    switch (windowMessage.name) {
      case WINDOW_MESSAGE_TYPE.PING:
        lunaId.value = windowMessage.id;
        origin.value = e.origin;

        sendEventToLuna(WINDOW_MESSAGE_TYPE.PONG, '', lunaId.value, origin.value);
        break;
      case WINDOW_MESSAGE_TYPE.OPEN:
        emits('update:drawer', true, t('Settings'), 'setting');
        break;
      case WINDOW_MESSAGE_TYPE.FILE:
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

  terminalInstance.open(terminalContainer);

  if (props.shareCode) {
    setShareCode(props.shareCode);
  }

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
