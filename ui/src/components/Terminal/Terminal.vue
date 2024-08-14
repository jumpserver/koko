<template>
    <n-layout style="height: 100vh">
        <n-scrollbar trigger="hover" style="max-height: 880px">
            <div id="terminal" class="terminal-container"></div>
        </n-scrollbar>
    </n-layout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { Terminal } from '@xterm/xterm';
import { onMounted, onUnmounted } from 'vue';
import { useTerminal } from '@/hooks/new_useTerminal.ts';

import type { ITerminalProps } from '@/hooks/interface';

import mittBus from '@/utils/mittBus.ts';

const { t } = useI18n();

const props = withDefaults(defineProps<ITerminalProps>(), {
    themeName: 'Default'
});

const emits = defineEmits<{
    (e: 'event', event: string, data: string): void;
    (e: 'background-color', backgroundColor: string): void;
    (e: 'wsData', msgType: string, msg: any, terminal: Terminal): void;
}>();

onMounted(() => {
    const theme = props.themeName;
    const el: HTMLElement = document.getElementById('terminal')!;

    const { createTerminal, setTerminalTheme, sendWsMessage } = useTerminal({
        i18nCallBack: (key: string) => t(key),
        emitCallback: (e: string, type: string, msg: any, terminal?: Terminal) => {
            switch (e) {
                case 'event': {
                    emits('event', type, msg);
                    break;
                }
                case 'wsData': {
                    emits('wsData', type, msg, terminal);
                    break;
                }
            }
        }
    });

    const { terminal } = createTerminal(el, 'common');

    console.log(terminal);

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

onUnmounted(() => {
    mittBus.off('set-theme');
    mittBus.off('sync-theme');
    mittBus.off('share-user');
    mittBus.off('create-share-url');
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
        height: 878px !important;
    }
}
</style>
