<template>
    <n-layout style="height: calc(100vh - 35px)">
        <n-scrollbar trigger="hover" style="max-height: 100vh">
            <div :id="indexKey" class="terminal-container"></div>
        </n-scrollbar>
    </n-layout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { Terminal } from '@xterm/xterm';
import { onMounted, onUnmounted, ref } from 'vue';
import { useTerminal } from '@/hooks/useTerminal.ts';

import type { ITerminalProps } from '@/hooks/interface';

import mittBus from '@/utils/mittBus.ts';

const { t } = useI18n();

const props = withDefaults(defineProps<ITerminalProps>(), {
    themeName: 'Default',
    terminalType: 'common'
});

const emits = defineEmits<{
    (e: 'event', event: string, data: string): void;
    (e: 'background-color', backgroundColor: string): void;
    (e: 'socketData', msgType: string, msg: any, terminal: Terminal): void;
}>();

const terminalRef = ref<any>(null);

onMounted(() => {
    const theme = props.themeName;
    const el: HTMLElement = document.getElementById(props.indexKey as string)!;

    const { createTerminal, setTerminalTheme, sendWsMessage } = useTerminal({
        terminalType: props.terminalType,
        transSocket: props.socket ? props.socket : undefined,
        i18nCallBack: (key: string) => t(key),
        emitCallback: (e: string, type: string, msg: any, terminal?: Terminal) => {
            switch (e) {
                case 'event': {
                    emits('event', type, msg);
                    break;
                }
                case 'socketData': {
                    emits('socketData', type, msg, terminal!);
                    break;
                }
            }
        }
    });

    const { terminal } = createTerminal(el);

    terminalRef.value = terminal;

    // 设置主题
    setTerminalTheme(theme, terminal, emits);

    // 修改主题
    mittBus.on('set-theme', ({ themeName }) => {
        setTerminalTheme(themeName as string, terminal, emits);
    });

    mittBus.on('sync-theme', ({ type, data }) => {
        sendWsMessage(type, data);
    });

    mittBus.on('share-user', ({ type, query }) => {
        sendWsMessage(type, { query });
    });

    mittBus.on('remove-share-user', ({ sessionId, userMeta, type }) => {
        sendWsMessage(type, {
            session: sessionId,
            user_meta: JSON.stringify(userMeta)
        });
    });

    mittBus.on('create-share-url', ({ type, sessionId, shareLinkRequest }) => {
        const origin = window.location.origin;

        sendWsMessage(type, {
            origin,
            session: sessionId,
            users: shareLinkRequest.users,
            expired_time: shareLinkRequest.expiredTime,
            action_permission: shareLinkRequest.actionPerm
        });
    });
});

defineExpose({
    terminalRef
});

onUnmounted(() => {
    mittBus.off('set-theme');
    mittBus.off('sync-theme');
    mittBus.off('share-user');
    mittBus.off('create-share-url');
    mittBus.off('remove-share-user');
});
</script>

<style scoped lang="scss">
.terminal-container {
    height: calc(100% - 10px);
    overflow: hidden;

    :deep(.xterm-viewport) {
        overflow: hidden;
    }

    :deep(.xterm-screen) {
        height: calc(100vh - 35px) !important;
        //height: 878px !important;
    }
}
</style>
