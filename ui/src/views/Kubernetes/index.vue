<template>
    <n-layout has-sider class="custom-layout h-full">
        <n-layout-header class="w-[40px]">
            <n-flex
                vertical
                align="center"
                justify="space-between"
                class="w-full h-full text-white bg-[#333333]"
            >
                <SideTop />
            </n-flex>
        </n-layout-header>
        <n-layout-sider
            bordered
            collapsed
            collapse-mode="width"
            content-style="padding: 24px;"
            v-draggable="sideWidth"
            :width="sideWidth"
            :collapsed-width="0"
            :native-scrollbar="false"
            :show-collapsed-content="false"
            :style="{
                width: sideWidth + 'px',
                maxWidth: '600px'
            }"
        >
            <FileManagement
                :class="{
                    'transition-opacity duration-200': true,
                    'opacity-0': isFolded,
                    'opacity-100': !isFolded
                }"
                @sync-load-node="handleSyncLoad"
            />
        </n-layout-sider>
        <MainContent :socket="socket" />
    </n-layout>
</template>

<script setup lang="ts">
// 使用 Hook
import { useK8s } from '@/hooks/useK8s';
import { TreeOption } from 'naive-ui';

import mittBus from '@/utils/mittBus';
import SideTop from '@/components/Kubernetes/Sidebar/sideTop.vue';
import MainContent from '@/components/Kubernetes/MainContent/index.vue';
import FileManagement from '@/components/Kubernetes/FileManagement/index.vue';

// 导入 API
import { onMounted, onUnmounted, ref } from 'vue';

const sideWidth = ref(300);
const isFolded = ref(false);

const handleClickOutSide = () => {};

const handleTreeClick = () => {
    isFolded.value = !isFolded.value;
    sideWidth.value = isFolded.value ? 0 : 300;
};

const { createTreeConnect, syncLoadNodes } = useK8s();

const socket = createTreeConnect();

const handleSyncLoad = (node: TreeOption) => {
    syncLoadNodes(node);
};

onMounted(() => {
    document.addEventListener('click', (_e: Event) => handleClickOutSide, false);

    mittBus.on('fold-tree-click', handleTreeClick);
});

onUnmounted(() => {
    document.removeEventListener('click', (_e: Event) => handleClickOutSide, false);

    mittBus.off('fold-tree-click', handleTreeClick);
});
</script>

<style scoped lang="scss">
@import './index.scss';
</style>
