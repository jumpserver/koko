<template>
    <n-flex justify="center" align="center" class="cursor-pointer w-full h-[48px]">
        <template v-for="option of topIconOptions" :key="option.name">
            <component :is="option.component" />
        </template>
    </n-flex>
    <n-flex justify="center" align="center" class="mb-[15px] cursor-pointer">
        <template v-for="option of bottomOptions" :key="option.name">
            <component :is="option.component" :on-click="option.onClick" />
        </template>
    </n-flex>
</template>

<script setup lang="ts">
import Tree from './components/Tree/index.vue';
import Setting from './components/Setting/index.vue';

import mittBus from '@/utils/mittBus.ts';

const topIconOptions = [
    {
        component: Tree
    }
];

const bottomOptions = [
    {
        component: Setting,
        onClick: () => {
            mittBus.emit('open-setting');
        }
    }
];
</script>

<style scoped lang="scss">
:deep(.n-flex) {
    gap: 15px 12px !important;
}
</style>
