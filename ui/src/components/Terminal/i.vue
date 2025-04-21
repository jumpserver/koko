<!-- <template>
  <div id="terminal-container" class="w-full h-full"></div>
</template>

<script setup lang="ts">
import xtermTheme from 'xterm-theme';

import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { Terminal } from '@xterm/xterm';
import { useWebSocket } from '@vueuse/core';
import { generateWsURL } from '@/hooks/helper';
import { onMounted, watch, onBeforeUnmount, ref } from 'vue';
import { sendEventToLuna } from '@/components/TerminalComponent/helper';
import { useTerminalInstance } from '@/hooks/useTerminalInstance';
import { useTerminalConnection } from '@/hooks/useTerminalConnection';
import { useConnectionStore } from '@/store/modules/useConnection';

import { WINDOW_MESSAGE_TYPE } from '@/enum';

const { t } = useI18n();
const message = useMessage();
const connectionStore = useConnectionStore();

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
        title.value = t('Settings');
        contentType.value = 'setting';

        showDrawer.value = true;
        break;
      case WINDOW_MESSAGE_TYPE.FILE:
        title.value = t('FileManager');
        contentType.value = 'file-manager';
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

  const {
    connectionStatus,
    initializeSocketEvent,
    handleCreateShareUrl,
    getShareUser,
    setShareCode,
    handeleRemoveShareUser
  } = useTerminalConnection(lunaId.value, origin.value);
  const { createTerminalInstance, terminalResizeEvent } = useTerminalInstance(socket.value);

  const terminalContainer: HTMLElement | null = document.getElementById('terminal-container');

  if (!terminalContainer) {
    return;
  }

  const terminalInstance: Terminal = createTerminalInstance(terminalContainer);

  terminalInstance.open(terminalContainer);

  initializeSocketEvent(terminalInstance, socket.value, t);
});
</script> -->
