<script setup lang="ts">
import { onUnmounted, provide } from 'vue';

import { createTerminalContext, terminalContextKey } from '@/context/terminalContext';

const terminalContext = createTerminalContext();

provide(terminalContextKey, terminalContext);

terminalContext.initialize();

onUnmounted(() => {
  terminalContext.cleanup();
});
</script>

<template>
  <div class="w-full h-full">
    <slot name="terminal" />
    <slot name="drawer" />
  </div>
</template>
