<template>
    <div id="terminal"></div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { loadConfig, createTerminal } from './helper';
import type { ILunaConfig, ITerminalProps } from '../interface';
import { useLogger } from '@/hooks/useLogger.ts';
import { FitAddon } from '@xterm/addon-fit';

const props = withDefaults(defineProps<ITerminalProps>(), {
    themeName: 'Default',
    enableZmodem: false
});

const fitAddon = new FitAddon();

const { info } = useLogger();

const config = ref<ILunaConfig>({});

const connect = async () => {
    info(`connectURL: ${props.connectURL}`);

    const el = document.getElementById('terminal');

    config.value = loadConfig();

    await createTerminal(config.value, el!, fitAddon);
};

onMounted(() => {
    connect();
});
</script>

<style scoped></style>
