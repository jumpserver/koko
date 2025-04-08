<template>
  <div id="terminal-container" class="w-full h-full"></div>
</template>

<script setup lang="ts">
import mittBus from '@/utils/mittBus.ts';
import xtermTheme from 'xterm-theme';

import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { onMounted, watch } from 'vue';
import { Terminal } from '@xterm/xterm';
import { useTerminalInstance } from '@/hooks/useTerminalInstance';
import { useTerminalConnection } from '@/hooks/useTerminalConnection';
import { formatMessage } from '@/components/TerminalComponent/helper';
import { useTerminalSettingsStore } from '@/store/modules/terminalSettings';

import type { ShareUserOptions } from '@/types';

const props = defineProps<{
  lunaId: string;
  origin: string;
  socketInstance?: WebSocket | '';
}>();

const emits = defineEmits<{
  (
    e: 'update:shareResult',
    shareResult: {
      shareId: string;
      shareCode: string;
    }
  ): void;
  (e: 'update:onlineUsers', onlineUsers: any[]): void;
  (e: 'update:shareEnable', shareEnable: boolean): void;
  (e: 'update:shareUserOptions', shareUserOptions: ShareUserOptions[]): void;
}>();

const { t } = useI18n();
const message = useMessage();
const terminalSettingsStore = useTerminalSettingsStore();
const { connectionStatus, initializeSocketEvent, handleCreateShareUrl, getShareUser, handeleRemoveShareUser } =
  useTerminalConnection(props.lunaId, props.origin);
const { createTerminalInstance, terminalResizeEvent } = useTerminalInstance(props.socketInstance);

onMounted(() => {
  const { terminalId, enableShare, onlineUsers } = connectionStatus;

  const terminalContainer: HTMLElement | null = document.getElementById('terminal-container');

  if (!terminalContainer) {
    return;
  }

  const terminalInstance: Terminal = createTerminalInstance(terminalContainer);

  terminalInstance.open(terminalContainer);

  if (!props.socketInstance) {
    return;
  }

  initializeSocketEvent(terminalInstance, props.socketInstance, t);

  mittBus.on('share-user', async ({ type, query }) => {
    if (!props.socketInstance) {
      return;
    }

    const userOptions: ShareUserOptions[] = await getShareUser(props.socketInstance, query);

    emits('update:shareUserOptions', userOptions);
  });
  mittBus.on('create-share-url', async ({ type, shareLinkRequest }: { type: string; shareLinkRequest: any }) => {
    if (!props.socketInstance) {
      return;
    }

    const { shareId, shareCode } = await handleCreateShareUrl(props.socketInstance, shareLinkRequest);

    emits('update:shareResult', {
      shareId,
      shareCode
    });
  });
  mittBus.on('remove-share-user', ({ type, userMeta }) => {
    if (!props.socketInstance) {
      return;
    }

    handeleRemoveShareUser(props.socketInstance, userMeta);
  });

  watch(
    () => enableShare.value,
    newValue => {
      emits('update:shareEnable', newValue);
    }
  );

  watch(
    () => terminalId.value,
    id => {
      if (id) {
        terminalResizeEvent(terminalId.value);
      }
    }
  );

  watch(
    () => terminalSettingsStore.theme,
    theme => {
      terminalInstance.options.theme = xtermTheme[theme!];

      if (!props.socketInstance) {
        return message.error('无法将主题同步到远端');
      }

      props.socketInstance?.send(
        formatMessage(
          terminalId.value,
          'TERMINAL_SYNC_USER_PREFERENCE',
          JSON.stringify({
            terminal_theme_name: theme
          })
        )
      );
    }
  );

  watch(
    () => onlineUsers.value,
    userMap => {
      emits('update:onlineUsers', userMap);
    },
    { deep: true }
  );
});
</script>

<style scoped lang="scss">
#terminal-container {
  :deep(.terminal) {
    height: 100%;
    padding: 10px 0 5px 10px;

    .xterm-viewport {
      overflow-y: unset !important;
    }
  }
}
</style>
