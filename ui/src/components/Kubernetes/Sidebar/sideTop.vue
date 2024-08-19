<template>
    <n-flex justify="center" align="center" class="w-full cursor-pointer">
        <template v-for="option of topIconOptions" :key="option.name">
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
import Logo from './components/Logo/index.vue';
import Setting from './components/Setting/index.vue';

import { CSSProperties, h } from 'vue';
import { useParamsStore } from '@/store/modules/params.ts';
import { storeToRefs } from 'pinia';
import mittBus from '@/utils/mittBus.ts';

const iconStyle: CSSProperties = {
    fill: '#646A73',
    width: '30px',
    height: '30px',
    transition: 'fill 0.3s'
};

const paramsStore = useParamsStore();
const { setting } = storeToRefs(paramsStore);

const topIconOptions = [
    {
        iconStyle,
        name: 'logo',
        component: () =>
            h(Logo, {
                logoImage: setting.value.INTERFACE?.logo_logout as string
            })
    },
    {
        iconStyle,
        name: 'tree',
        component: Tree
    },
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

    .tree-icon:hover {
        fill: #1ab394 !important;
    }
}
</style>
