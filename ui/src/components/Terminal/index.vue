<template>
  <div id="terminal-container" class="w-screen h-screen"></div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { Terminal } from '@xterm/xterm';
import { onMounted, onUnmounted, ref } from 'vue';
import { useWebSocket } from '@vueuse/core';
import { generateWsURL } from '@/hooks/helper';
import { formatMessage } from '@/utils';
import { useTerminalInstance } from '@/hooks/useTerminalInstance';
import { useConnectionStore } from '@/store/modules/useConnection';
import { useTerminalConnection } from '@/hooks/useTerminalConnection';
import { LUNA_MESSAGE_TYPE, FORMATTER_MESSAGE_TYPE } from '@/types/modules/message.type';

import { LunaMessage } from '@/types/modules/postmessage.type';
import { lunaCommunicator } from '@/utils/lunaBus';

const props = defineProps<{
  shareCode?: string;

}>();

const { t } = useI18n();
const message = useMessage();
const connectionStore = useConnectionStore();

const socket = ref<WebSocket | ''>('');
const terminal = ref<Terminal | null>(null);


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



onMounted(() => {

  socket.value = createSocket();
  if (!socket.value) {
    return;
  }

  const { initializeSocketEvent, setShareCode, terminalResizeEvent } = useTerminalConnection();
  const { createTerminalInstance, fitAddon } = useTerminalInstance(socket.value);

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

  initializeSocketEvent(terminalInstance, socket.value, t);
  terminalResizeEvent(terminalInstance, socket.value, fitAddon);

  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.CMD, (msg: LunaMessage) => {
    if (typeof socket.value !== 'string') {
      const terminalId = Array.from(connectionStore.connectionStateMap.values())[0].terminalId || '';
      socket.value?.send(formatMessage(terminalId, FORMATTER_MESSAGE_TYPE.TERMINAL_DATA, msg.data));
    }
  });
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.FOCUS, (msg: LunaMessage) => {
    terminal.value?.focus();
  });
})


onUnmounted(() => {
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.CMD);
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.FOCUS);
});

</script>

<style scoped lang="scss">
#terminal-container {
  :deep(.terminal) {
    height: 100%;
    padding: 10px 0 5px 10px;
  }

  :deep(.xterm-viewport) {
    background-color: #000000;

    &::-webkit-scrollbar {
      height: 4px;
      width: 7px;
    }

    &::-webkit-scrollbar-track {
      background: #000000;
    }

    &::-webkit-scrollbar-thumb {
      background: rgba(255, 255, 255, 0.35);
      border-radius: 3px;
    }

    &::-webkit-scrollbar-thumb:hover {
      background: rgba(255, 255, 255, 0.5);
    }
  }
}
</style>
