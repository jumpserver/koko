<script setup lang="ts">
import { ref } from 'vue';
import { Kubernetes } from '@vicons/carbon';

import mittBus from '@/utils/mittBus';

const isActive = ref(true);

function handleTreeIconClick() {
  mittBus.emit('fold-tree-click');
  isActive.value = !isActive.value;
}
</script>

<template>
  <n-button text class="py-[5px] w-full icon-wrapper" :class="{ active: isActive }" @click="handleTreeIconClick">
    <n-icon
      :component="Kubernetes"
      color="#646A73"
      size="30"
      class="hover:!text-white transition-all duration-300 cursor-pointer tree-icon"
    />
  </n-button>
</template>

<style scoped lang="scss">
@use './index.scss';
</style>
