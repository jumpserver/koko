<script setup lang="ts">
import Drawer from '@/components/Drawer/index.vue';
import Terminal from '@/components/Terminal/index.vue';
import TerminalProvider from '@/components/TerminalProvider/index.vue';
</script>

<template>
  <div class="h-full w-full">
    <TerminalProvider>
      <template #terminal>
        <Terminal />
      </template>

      <template #drawer>
        <Drawer :show-drawer="false" />
      </template>
    </TerminalProvider>
  </div>
</template>
