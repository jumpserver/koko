<template>
    <n-layout has-sider class="custom-layout h-full">
        <n-layout-header class="w-[40px]">
            <n-flex
                class="w-full h-full text-white bg-[#333333]"
                vertical
                align="center"
                justify="space-between"
            >
                <SideTop />
            </n-flex>
        </n-layout-header>
        <n-layout-sider
            v-draggable="sideWidth"
            bordered
            collapsed
            collapse-mode="width"
            content-style="padding: 24px;"
            class="transition-all duration-300"
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
                :treeNodes="treeNodes"
                :class="{
                    'transition-opacity duration-200': true,
                    'opacity-0': isFolded,
                    'opacity-100': !isFolded
                }"
                @sync-load-node="handleSyncLoadNode"
            />
        </n-layout-sider>
        <MainContent
            :socket="socket"
            :terminal-id="terminalId"
            :socket-send="socketSend"
            :socket-data="socketData"
        />
    </n-layout>
</template>

<script setup lang="ts">
// 使用 Hook
import { useTreeStore } from '@/store/modules/tree.ts';
import { useWebSocket as customUseWebSocket } from '@/hooks/useWebSocket.ts';

import type { Ref } from 'vue';
import type { TreeOption } from 'naive-ui';
import type { customTreeOption } from '@/hooks/interface';

import mittBus from '@/utils/mittBus.ts';
import SideTop from '@/components/Kubernetes/Sidebar/sideTop.vue';
import MainContent from '@/components/Kubernetes/MainContent/index.vue';
import FileManagement from '@/components/Kubernetes/FileManagement/index.vue';

// 导入 API
import { onMounted, onUnmounted, ref } from 'vue';

// 额外项
import { useI18n } from 'vue-i18n';
import { storeToRefs } from 'pinia';

const { t } = useI18n();

const treeStore = useTreeStore();
const {} = storeToRefs(treeStore);

let socketData: Ref<any>;
let treeNodes: Ref<TreeOption[]> = ref([]);
let lastSendTime: Ref<Date> = ref(new Date());
let socket: Ref<WebSocket | null> = ref(null);
let socketSend: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean;

const enableZmodem = ref(true);
const zmodemStatus = ref(false);

const terminalId = ref('');
const sideWidth = ref(300);
const isFolded = ref(false);
const connectInfo = ref<any | null>(null);

const { createWebSocket } = customUseWebSocket(terminalId, {
    enableZmodem: enableZmodem.value,
    zmodemStatus,
    isK8s: true
});

const handleClickOutSide = () => {};
const handleSyncLoadNode = (treeNodes: customTreeOption) => {
    if (socket.value) {
        treeStore.loadTreeNode(socket.value, socketSend, treeNodes, terminalId.value);
    }
};
const handleTreeClick = () => {
    isFolded.value = !isFolded.value;
    sideWidth.value = isFolded.value ? 0 : 300;
};

const initConnection = () => {
    const { ws, send, data } = createWebSocket(lastSendTime, t);

    socket.value = ws.value!;
    socketSend = send;
    socketData = data;
};

initConnection();

onMounted(() => {
    document.addEventListener('click', (e: Event) => handleClickOutSide, false);

    mittBus.on('fold-tree-click', handleTreeClick);
});

onUnmounted(() => {
    document.removeEventListener('click', (e: Event) => handleClickOutSide, false);

    mittBus.off('fold-tree-click', handleTreeClick);
});
</script>

<style scoped lang="scss">
@import './index.scss';
</style>
