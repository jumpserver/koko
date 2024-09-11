<template>
    <CustomTerminal index-key="id" class="common-terminal" :terminal-type="terminalType" />
</template>

<script setup lang="ts">
import { ref } from 'vue';
import CustomTerminal from '@/components/CustomTerminal/index.vue';

const terminalType = ref<string>('common');
</script>

<style lang="scss" scoped>
.common-terminal {
    :deep(.terminal-container) {
        overflow: hidden;

        .xterm-viewport {
            overflow: hidden;
        }

        .xterm-screen {
            height: calc(100vh - 20px) !important;
        }
    }
}
</style>
