<template>
  <div id="terminal-container" class="w-full h-full"></div>
</template>

<script setup lang="ts">
import mittBus from '@/utils/mittBus.ts';
import xtermTheme from 'xterm-theme';

import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { Terminal } from '@xterm/xterm';
import { readText } from 'clipboard-polyfill';
import { onMounted, watch, onBeforeUnmount } from 'vue';
import { useTerminalInstance } from '@/hooks/useTerminalInstance';
import { useTerminalConnection } from '@/hooks/useTerminalConnection';
import { formatMessage } from '@/components/TerminalComponent/helper';
import { useTerminalSettingsStore } from '@/store/modules/terminalSettings';

import type { ShareUserOptions } from '@/types/modules/user.type';

const props = defineProps<{
  lunaId?: string;
  origin?: string;
  shareCode?: string;
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
const {
  connectionStatus,
  initializeSocketEvent,
  handleCreateShareUrl,
  getShareUser,
  setShareCode,
  handeleRemoveShareUser
} = useTerminalConnection(props.lunaId!, props.origin!);
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
  mittBus.on('writeDataToTerminal', async ({ type }) => {
    switch (type) {
      case 'Stop':
        terminalInstance.paste('\x03');
        break;
      case 'Paste':
        terminalInstance.paste(await readText());
        break;
      case 'ArrowUp':
        terminalInstance.paste('\x1b[A');
        break;
      case 'ArrowDown':
        terminalInstance.paste('\x1b[B');
        break;
      case 'ArrowLeft':
        terminalInstance.paste('\x1b[D');
        break;
      case 'ArrowRight':
        terminalInstance.paste('\x1b[C');
        break;
    }
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
    () => props.shareCode,
    code => {
      if (code) {
        setShareCode(code);
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

onBeforeUnmount(() => {
  mittBus.off('share-user');
  mittBus.off('create-share-url');
  mittBus.off('remove-share-user');
  mittBus.off('writeDataToTerminal');
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
