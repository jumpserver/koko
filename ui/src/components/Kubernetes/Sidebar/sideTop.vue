<template>
    <n-flex justify="center" align="center" class="w-full cursor-pointer">
        <template v-for="option of topIconOptions" :key="option.name">
            <component :is="option.component" :name="option.name" :icon-style="option.iconStyle" />
        </template>
    </n-flex>
</template>

<script setup lang="ts">
import Tree from './components/Tree/index.vue';
import Organization from './components/Organization/index.vue';

import type { CSSProperties } from 'vue';

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
    },
    {
        iconStyle,
        name: 'organize',
        component: Organization
    }
];
</script>

<style scoped lang="scss">
:deep(.n-flex) {
    gap: 15px 12px !important;

    .tree-icon:hover {
        fill: #1ab394 !important;
    }
}
</style>
