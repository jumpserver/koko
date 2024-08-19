<template>
    <n-layout :native-scrollbar="false" content-style="height: 100%">
        <n-tabs
            closable
            size="small"
            type="card"
            tab="show:lazy"
            tab-style="min-width: 80px;"
            v-model:value="nameRef"
            @close="handleClose"
            @before-leave="handleBeforeLeave"
            @update:value="handleChangeTab"
            class="header-tab relative"
        >
            <n-tab-pane
                v-for="panel of panels"
                :key="panel.name"
                :tab="panel.tab"
                :name="panel.name"
                class="bg-[#101014] pt-0"
            >
                <n-scrollbar trigger="hover">
                    <keep-alive>
                        <TerminalComponent ref="terminalRef" terminal-type="k8s" :socket="socket" />
                    </keep-alive>
                </n-scrollbar>
            </n-tab-pane>
            <template v-slot:suffix>
                <n-flex
                    justify="space-between"
                    align="center"
                    class="h-[35px] mr-[15px]"
                    style="column-gap: 5px"
                >
                    <n-popover>
                        <template #trigger>
                            <div
                                class="icon-item flex justify-center items-center w-[25px] h-[25px] cursor-pointer transition-all duration-300 ease-in-out hover:rounded-[5px]"
                            >
                                <svg-icon name="split" :icon-style="iconStyle" />
                            </div>
                        </template>
                        拆分
                    </n-popover>

                    <n-popover>
                        <template #trigger>
                            <div
                                class="icon-item flex justify-center items-center w-[25px] h-[25px] cursor-pointer transition-all duration-300 ease-in-out hover:rounded-[5px]"
                            >
                                <n-icon size="16px" :component="EllipsisHorizontal" />
                            </div>
                        </template>
                        操作
                    </n-popover>
                </n-flex>
            </template>
        </n-tabs>
    </n-layout>
</template>

<script setup lang="ts">
import type { CSSProperties } from 'vue';
import { onBeforeUnmount, onMounted, Ref, ref, watch } from 'vue';

import TerminalComponent from '@/components/Terminal/Terminal.vue';

// 引入 type
import type { TabPaneProps } from 'naive-ui';
// 引入 hook
import { useMessage } from 'naive-ui';
import { useLogger } from '@/hooks/useLogger.ts';

import mittBus from '@/utils/mittBus.ts';
import { EllipsisHorizontal } from '@vicons/ionicons5';
import { updateIcon } from '@/components/Terminal/helper';
import { useTreeStore } from '@/store/modules/tree.ts';
import { storeToRefs } from 'pinia';

// 图标样式
const iconStyle: CSSProperties = {
    width: '16px',
    height: '16px',
    transition: 'fill 0.3s'
};

// 创建消息和日志实例
const message = useMessage();
const { debug } = useLogger('K8s-Terminal');

// 相关状态
const nameRef = ref('');
const terminalRef: Ref<any[]> = ref([]);

const panels: Ref<TabPaneProps[]> = ref([]);

const props = defineProps<{
    socket: WebSocket | undefined;
}>();

const treeStore = useTreeStore();

const { connectInfo } = storeToRefs(treeStore);

// 处理关闭标签页事件
const handleClose = (name: string) => {
    message.info(`已关闭: ${name}`);
    const index = panels.value.findIndex(panel => panel.name === name);
    panels.value.splice(index, 1);
};

const handleBeforeLeave = (tabName: string) => {
    console.log('Before', tabName);

    return true;
};

const handleChangeTab = (value: string) => {
    console.log('ing', value);
    nameRef.value = value;
};

// 监听连接终端事件
onMounted(() => {
    mittBus.on('connect-terminal', currentNode => {
        panels.value.push({
            name: currentNode.key,
            tab: currentNode.label
        });

        const sendTerminalData = () => {
            if (terminalRef.value) {
                console.log('terminalInstance', terminalRef.value);

                const terminalInstance = terminalRef.value[0]?.terminalRef; // 获取子组件的 terminal 实例
                const cols = terminalInstance?.cols;
                const rows = terminalInstance?.rows;

                if (cols && rows) {
                    const sendData = {
                        id: currentNode.id,
                        k8s_id: currentNode.k8s_id,
                        namespace: currentNode.namespace,
                        pod: currentNode.pod,
                        container: currentNode.container,
                        type: 'TERMINAL_K8S_INIT',
                        data: JSON.stringify({
                            cols,
                            rows,
                            code: ''
                        })
                    };

                    updateIcon(connectInfo.value.setting);
                    props.socket?.send(JSON.stringify(sendData));
                } else {
                    console.error('Failed to get terminal dimensions');
                }
            } else {
                console.error('Terminal ref is not available');
            }
        };

        // 立即发送数据
        sendTerminalData();

        // 监听 terminalRef 的变化，如果 terminal 实例准备好，再次发送数据
        watch(
            () => terminalRef.value,
            newValue => {
                if (newValue) {
                    sendTerminalData();
                }
            }
        );

        nameRef.value = currentNode.key as string;

        debug('currentNode', currentNode);
    });
});

onBeforeUnmount(() => {
    mittBus.off('connect-terminal');
});
</script>

<style scoped lang="scss">
@import './index.scss';
</style>
