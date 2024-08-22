<template>
    <n-button
        text
        @click="handleTreeIconClick"
        class="py-[5px] w-full icon-wrapper"
        :class="{ active: isActive }"
    >
        <svg-icon class="tree-icon" :name="name" :icon-style="iconStyle" />
    </n-button>
</template>

<script setup lang="ts">
import { CSSProperties, ref } from 'vue';

import mittBus from '@/utils/mittBus.ts';
import SvgIcon from '@/components/SvgIcon/index.vue';

defineProps<{
    name: string;
    iconStyle: CSSProperties;
}>();

const isActive = ref(true);

const handleTreeIconClick = () => {
    mittBus.emit('fold-tree-click');
    isActive.value = !isActive.value;
};
</script>

<style scoped lang="scss">
@import './index.scss';
</style>
