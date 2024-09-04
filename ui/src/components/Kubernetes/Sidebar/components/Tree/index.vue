<template>
    <n-button
        text
        @click="handleTreeIconClick"
        class="py-[5px] w-full icon-wrapper"
        :class="{ active: isActive }"
    >
        <n-icon
            :component="Kubernetes"
            color="#646A73"
            size="30"
            class="hover:!text-white transition-all duration-300 cursor-pointer tree-icon"
        />
    </n-button>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { Kubernetes } from '@vicons/carbon';

import mittBus from '@/utils/mittBus.ts';

const isActive = ref(true);

const handleTreeIconClick = () => {
    mittBus.emit('fold-tree-click');
    isActive.value = !isActive.value;
};
</script>

<style scoped lang="scss">
@import './index.scss';
</style>
