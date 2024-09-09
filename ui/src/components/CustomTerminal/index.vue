<template>
    <n-layout :native-scrollbar="false">
        <n-scrollbar trigger="hover">
            <div :id="indexKey" class="terminal-container"></div>
        </n-scrollbar>
    </n-layout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { onMounted, onUnmounted, ref } from 'vue';
import { useTerminal } from '@/hooks/useTerminal.ts';

import { Terminal } from '@xterm/xterm';
import { ITerminalProps } from '@/hooks/interface';

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

const terminalRef = ref<Terminal | undefined>(undefined);

onMounted(async () => {
    const theme = props.themeName;
    const el: HTMLElement = document.getElementById(props.indexKey as string)!;

    const { terminal, setTerminalTheme } = await useTerminal(el, {
        type: props.terminalType,
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

    terminalRef.value = terminal;

    // 设置主题
    setTerminalTheme(theme, terminal!, emits);

    // 修改主题
    mittBus.on('set-theme', ({ themeName }) => {
        setTerminalTheme(themeName as string, terminal!, emits);
    });
});

defineExpose({
    terminalRef
});

onUnmounted(() => {
    mittBus.off('set-theme');
});
</script>

<style lang="scss" scoped>
:deep(.terminal-container) {
    .xterm {
        padding-left: 10px;
        padding-top: 10px;
    }
}
</style>
