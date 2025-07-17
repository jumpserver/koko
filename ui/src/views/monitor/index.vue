<script setup lang="ts">
import Terminal from '@/components/Terminal/index.vue';
import TerminalProvider from '@/components/TerminalProvider/index.vue';
</script>

<template>
  <TerminalProvider>
    <template #terminal>
      <Terminal />
    </template>
  </TerminalProvider>
</template>
