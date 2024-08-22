<template>
    <n-flex justify="center" align="center" class="cursor-pointer w-full h-[48px]">
        <template v-for="option of topIconOptions" :key="option.name">
            <component :is="option.component" :name="option.name" :icon-style="option.iconStyle" />
        </template>
    </n-flex>
    <n-flex justify="center" align="center" class="mb-[15px] cursor-pointer">
        <template v-for="option of bottomOptions" :key="option.name">
            <component
                :is="option.component"
                :name="option.name"
                :on-click="option.onClick"
                :icon-style="option.iconStyle"
            />
        </template>
    </n-flex>
</template>

<script setup lang="ts">
import Tree from './components/Tree/index.vue';
import Setting from './components/Setting/index.vue';

import { CSSProperties } from 'vue';
import mittBus from '@/utils/mittBus.ts';

const iconStyle: CSSProperties = {
    fill: '#646A73',
    width: '30px',
    height: '30px',
    transition: 'fill 0.3s'
};

const topIconOptions = [
    {
        iconStyle,
        name: 'tree',
        component: Tree
    }
];

const bottomOptions = [
    {
        iconStyle,
        name: 'setting',
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
