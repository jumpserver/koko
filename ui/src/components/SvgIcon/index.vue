<template>
  <svg :style="iconStyle" aria-hidden="true">
    <use :xlink:href="symbolId" />
  </svg>
</template>

<script setup lang="ts">
// https://github.com/vbenjs/vite-plugin-svg-icons/blob/main/README.zh_CN.md
import { computed, CSSProperties } from 'vue';

interface SvgProps {
  // 图标的名称 ==> 必传
  name: string;

  // 图标的前缀 ==> 非必传（默认为"icon"）
  prefix?: string;

  // 图标的样式 ==> 非必传
  iconStyle?: CSSProperties;
}

const props = withDefaults(defineProps<SvgProps>(), {
  prefix: 'icon',
  iconStyle: () => ({ width: '30px', height: '30px' })
});

const symbolId = computed(() => `#${props.prefix}-${props.name}`);
</script>

<style lang="scss" scoped>
svg:focus {
  outline: unset;
}
</style>
