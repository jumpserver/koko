<template>
  <Terminal :socket-instance="socketInstance" :lunaId="lunaId" :origin="origin" />
</template>

<script setup lang="ts">
import Terminal from '@/components/Terminal/index.vue';
import { onMounted, ref } from 'vue';
import { useWebSocketManager } from '@/hooks/useWebSocketManager';
import { sendEventToLuna } from '@/components/TerminalComponent/helper';

enum WindowMessageType {
  PING = 'PING',
  PONG = 'PONG',
  CMD = 'CMD',
  FOCUS = 'FOCUS',
  OPEN = 'OPEN',
  FILE = 'FILE',
}

const { createSocket } = useWebSocketManager();

const lunaId = ref<string>('');
const origin = ref<string>('');
const socketInstance = ref<WebSocket | ''>();

const initializeWindowEvent = () => {
  window.addEventListener('message', (e: MessageEvent) => {
    const windowMessage = e.data;

    switch (windowMessage.name) {
      case WindowMessageType.PING: {
        lunaId.value = windowMessage.id;
        origin.value = e.origin;

        sendEventToLuna(WindowMessageType.PONG, '', lunaId.value, origin.value);
        break;
      }
    }
  })
}

socketInstance.value = createSocket();

onMounted(() => {
  initializeWindowEvent();
});
</script>
