<script setup lang="ts">
import { ref } from 'vue';

import { useTerminalEvents } from '@/hooks/useTerminalEvents';
import { useTerminalSocket } from '@/hooks/useTerminalSocket';

const showSearchInput = ref(false);

const { onMittEvent } = useTerminalEvents();
const { containerRef, searchAddon } = useTerminalSocket();

onMittEvent('open-search', () => {
  showSearchInput.value = true;
});
</script>

<template>
  <SearchInput v-if="showSearchInput" :search-addon="searchAddon" @close="showSearchInput = false" />

  <div id="terminal-container" ref="containerRef" class="w-screen h-screen" />
</template>

<style scoped lang="scss">
#terminal-container {
  :deep(.terminal) {
    height: 100%;
    padding: 10px 0 5px 10px;
  }

  :deep(.xterm-viewport) {
    &::-webkit-scrollbar {
      height: 4px;
      width: 7px;
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
