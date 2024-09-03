<template>
    <ContentHeader />
    <n-layout has-sider class="custom-layout h-full">
        <n-layout-header class="w-[48px]">
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
            class="transition-width duration-300"
            :width="sideWidth"
            :collapsed-width="0"
            :native-scrollbar="false"
            :show-collapsed-content="false"
            :style="{
                width: sideWidth + 'px',
                maxWidth: '600px'
            }"
        >
            <Tree
                :class="{
                    'transition-opacity duration-200': true,
                    'opacity-0': isFolded,
                    'opacity-100': !isFolded
                }"
                @sync-load-node="handleSyncLoad"
                @reload-tree="handleReloadTree"
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
import Tree from '@/components/Kubernetes/Tree/index.vue';
import SideTop from '@/components/Kubernetes/Sidebar/sideTop.vue';
import MainContent from '@/components/Kubernetes/MainContent/index.vue';
import ContentHeader from '@/components/Kubernetes/ContentHeader/index.vue';

import { onMounted, onUnmounted, ref } from 'vue';
import { BASE_WS_URL } from '@/config';

const socket = ref();
const sideWidth = ref(300);
const isFolded = ref(false);

const handleTreeClick = () => {
    isFolded.value = !isFolded.value;
    sideWidth.value = isFolded.value ? 0 : 300;
};

const { createTreeConnect, syncLoadNodes, reload } = useK8s();

socket.value = createTreeConnect();

const handleSyncLoad = (node: TreeOption) => {
    syncLoadNodes(node);

    // 根据节点宽度自动拓宽
    setTimeout(() => {
        const tableElement = document.querySelector('.n-descriptions-table') as HTMLElement;
        const sideElement = document.querySelector('.n-layout-sider') as HTMLElement;

        if (tableElement && sideElement) {
            const tableWidth = tableElement.clientWidth;

            sideElement.style.width = `${tableWidth}px`;
        }
    }, 300);
};

const handleReloadTree = () => {
    if (socket.value) {
        socket.value.close();
    }

    const urlParams = new URLSearchParams(window.location.search.slice(1));
    const connectURL = urlParams ? `${BASE_WS_URL}/koko/ws/token/?${urlParams.toString()}` : '';

    reload(connectURL);
};

onMounted(() => {
    mittBus.on('fold-tree-click', handleTreeClick);
});

onUnmounted(() => {
    mittBus.off('fold-tree-click', handleTreeClick);
});
</script>

<style scoped lang="scss">
@import './index.scss';
</style>
