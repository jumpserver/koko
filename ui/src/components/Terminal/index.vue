<template>
  <div id="terminal-container" class="w-full h-full"></div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue';
import { useMessage} from 'naive-ui';
import { Terminal } from '@xterm/xterm';
import { useTerminalInstance } from '@/hooks/useTerminalInstance';
import { useTerminalConnection } from '@/hooks/useTerminalConnection';

const props = defineProps<{
  lunaId: string;
  origin: string;
  socket?: WebSocket | '';
}>();

const message = useMessage()
const { createTerminalInstance } = useTerminalInstance();
const { initializeSocketEvent} = useTerminalConnection(props.lunaId, props.origin)

onMounted(() => {
  const terminalContainer: HTMLElement | null = document.getElementById('terminal-container');

  if (!terminalContainer) {
    return;
  }

  const terminalInstance: Terminal = createTerminalInstance(terminalContainer);

  terminalInstance.open(terminalContainer);

  if (!props.socket) {
    return
  }

  initializeSocketEvent(terminalInstance, props.socket);
});
</script>

<style scoped lang="scss">
#terminal-container {
  :deep(.terminal) {
    padding: 10px 0 5px 10px;

    .xterm-viewport {
      overflow-y: unset !important;
    }
  }
}
</style>
