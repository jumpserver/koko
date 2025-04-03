<template>
  <div id="terminal-container" class="w-full h-full"></div>
</template>

<script setup lang="ts">
import { onMounted, watch } from 'vue';
import { useMessage } from 'naive-ui';
import { Terminal } from '@xterm/xterm';
import { useTerminalInstance } from '@/hooks/useTerminalInstance';
import { useTerminalConnection } from '@/hooks/useTerminalConnection';

const props = defineProps<{
  lunaId: string;
  origin: string;
  socketInstance?: WebSocket | '';
}>();

const message = useMessage();
const { terminalId, initializeSocketEvent } = useTerminalConnection(props.lunaId, props.origin);
const { createTerminalInstance, terminalResizeEvent } = useTerminalInstance(props.socketInstance);

onMounted(() => {
  watch(
    () => terminalId.value,
    id => {
      if (id) {
        terminalResizeEvent(terminalId.value);
      }
    }
  );

  const terminalContainer: HTMLElement | null = document.getElementById('terminal-container');

  if (!terminalContainer) {
    return;
  }

  const terminalInstance: Terminal = createTerminalInstance(terminalContainer);

  terminalInstance.open(terminalContainer);

  if (!props.socketInstance) {
    return;
  }

  initializeSocketEvent(terminalInstance, props.socketInstance);
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
