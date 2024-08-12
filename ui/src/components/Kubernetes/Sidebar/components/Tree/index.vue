<template>
  <n-button text @click="handleTreeIconClick" class="py-[5px] icon-wrapper">
    <svg-icon class="tree-icon" :name="name" :icon-style="iconStyle" />
  </n-button>
</template>

<script setup lang="ts">
import type { CSSProperties } from 'vue';

import mittBus from '@/utils/mittBus.ts';
import SvgIcon from '@/components/SvgIcon/index.vue';

defineProps<{
  name: string;
  iconStyle: CSSProperties;
}>();

const handleTreeIconClick = () => {
  mittBus.emit('fold-tree-click');
};
</script>

<style scoped lang="scss">
@import './index.scss';
</style>
