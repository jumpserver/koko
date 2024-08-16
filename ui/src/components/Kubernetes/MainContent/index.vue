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
                    <TerminalComponent />
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
        </n-tabs> </n-layout
    >≠
</template>

<script setup lang="ts">
import type { CSSProperties } from 'vue';
import { nextTick, onBeforeUnmount, onMounted, Ref, ref, watch } from 'vue';

import TerminalComponent from '@/components/Terminal/Terminal.vue';

// 引入 type
import type { TabPaneProps } from 'naive-ui';
// 引入 hook
import { useMessage } from 'naive-ui';
import { useLogger } from '@/hooks/useLogger.ts';
import { useSentry } from '@/hooks/useZsentry.ts';
import { useTerminal } from '@/hooks/useTerminal.ts';

// 引入 store
import { useTerminalStore } from '@/store/modules/terminal.ts';
import type { customTreeOption, ILunaConfig } from '@/hooks/interface';

import mittBus from '@/utils/mittBus.ts';
import { EllipsisHorizontal } from '@vicons/ionicons5';
import { updateIcon } from '@/components/Terminal/helper';
import { base64ToUint8Array } from '@/components/Kubernetes/helper';
import { Sentry } from 'nora-zmodemjs/src/zmodem_browser';
import { Terminal } from '@xterm/xterm';
import { useI18n } from 'vue-i18n';
import { useTreeStore } from '@/store/modules/tree.ts';
import { storeToRefs } from 'pinia';

// 图标样式
const iconStyle: CSSProperties = {
    width: '16px',
    height: '16px',
    transition: 'fill 0.3s'
};

// 获取 props
const props = defineProps<{
    socket: WebSocket | null;
    terminalId?: string;
    socketData?: any;
    socketSend?: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean;
}>();

// 创建消息和日志实例
const message = useMessage();
const { debug } = useLogger('K8s-Terminal');

// 相关状态
const nameRef = ref('');
const enableZmodem = ref(true);
const zmodemStatus = ref(false);

const lastSendTime: Ref<Date> = ref(new Date());
const lunaConfig: Ref<ILunaConfig> = ref({});
const sentryRef: Ref<Sentry | undefined> = ref(undefined);
const terminalRef: Ref<Terminal | undefined> = ref(undefined);

const panels: Ref<TabPaneProps[]> = ref([]);
const termianlIdRef = ref('');

const { t } = useI18n();

const treeStore = useTreeStore();

const { connectInfo } = storeToRefs(treeStore);

watch(
    () => props.socketData,
    (newValue: any) => {
        handleSocketData(JSON.parse(newValue));
    }
);

watch(
    () => props.terminalId,
    newValue => {
        termianlIdRef.value = newValue;
        console.log('newid', newValue);
    }
);

const handleSocketData = (socketData: any) => {
    switch (socketData.type) {
        case 'TERMINAL_K8S_BINARY': {
            sentryRef.value?.consume(base64ToUint8Array(socketData.raw));
            break;
        }
        case 'TERMINAL_ACTION': {
            const action = socketData.data;
            switch (action) {
                case 'ZMODEM_START': {
                    zmodemStatus.value = true;
                    if (!enableZmodem.value) {
                        message.warning(t('Terminal.WaitFileTransfer'));
                    }
                    break;
                }
                case 'ZMODEM_END': {
                    if (!enableZmodem.value && zmodemStatus.value) {
                        message.warning(t('Terminal.EndFileTransfer'));
                        terminalRef.value?.writeln('\r\n');
                    }

                    zmodemStatus.value = false;
                    break;
                }
                default: {
                    zmodemStatus.value = false;
                }
            }
            break;
        }
        case 'TERMINAL_ERROR': {
            message.error(`Socket Error ${socketData.err}`);
            terminalRef.value?.writeln(socketData.err);
            break;
        }
        default: {
            debug('Default Handle SocketData Switch', socketData);
        }
    }
};

// 终端相关函数
const { createTerminal, initTerminalEvent } = useTerminal(termianlIdRef, 'k8s');
const { createSentry } = useSentry(lastSendTime);

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

// 创建 K8s 终端
const createK8sTerminal = async (currentNode: customTreeOption) => {
    await nextTick();

    const terminalId = `terminal-${currentNode.key}`;
    const el: HTMLElement = document.getElementById(terminalId)!;

    const terminalStore = useTerminalStore();
    lunaConfig.value = terminalStore.getConfig;

    const { terminal, fitAddon } = createTerminal(el, lunaConfig.value);

    terminalRef.value = terminal;

    if (props.socket) {
        sentryRef.value = createSentry(props.socket, terminal);

        initTerminalEvent(props.socket, el, terminal, lunaConfig.value);

        const sendData = {
            id: props.terminalId,
            k8s_id: currentNode.k8s_id,
            namespace: currentNode.namespace,
            pod: currentNode.pod,
            container: currentNode.container,
            type: 'TERMINAL_K8S_INIT',
            data: JSON.stringify({
                cols: terminal.cols,
                rows: terminal.rows,
                code: ''
            })
        };

        debug(`Current User: ${connectInfo.value.user}`);

        updateIcon(connectInfo.value.setting);

        props.socketSend(JSON.stringify(sendData));
    }
};

// 监听连接终端事件
onMounted(() => {
    mittBus.on('connect-terminal', currentNode => {
        panels.value.push({
            name: currentNode.key,
            tab: currentNode.label
        });

        nameRef.value = currentNode.key as string;

        debug('currentNode', currentNode);

        createK8sTerminal(currentNode);
    });
});

onBeforeUnmount(() => {
    mittBus.off('connect-terminal');
});
</script>

<style scoped lang="scss">
@import './index.scss';
</style>
