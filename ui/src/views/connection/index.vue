<script setup lang="ts">
import Drawer from '@/components/Drawer/index.vue';
import Terminal from '@/components/Terminal/index.vue';



</script>

<template>
  <div class="h-full w-full">
    <Terminal />
    <Drawer :show-drawer="true" />
  </div>
</template>
