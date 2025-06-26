<script setup lang="ts">
import type { Terminal } from '@xterm/xterm';

import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { SearchAddon } from '@xterm/addon-search';
import { useWebSocket, useWindowSize } from '@vueuse/core';
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue';

import type { LunaEventType } from '@/utils/lunaBus';
import type { LunaMessage, ShareUserRequest, TerminalSessionInfo } from '@/types/modules/postmessage.type';

import { formatMessage } from '@/utils';
import { defaultTheme } from '@/utils/config';
import { generateWsURL } from '@/hooks/helper';
import { lunaCommunicator } from '@/utils/lunaBus';
import { useTerminalCreate } from '@/hooks/useTerminalCreate.ts';
import { useTerminalConnection } from '@/hooks/useTerminalConnection';
import { useTerminalSettingsStore } from '@/store/modules/terminalSettings.ts';
import { FORMATTER_MESSAGE_TYPE, LUNA_MESSAGE_TYPE } from '@/types/modules/message.type';

const props = defineProps<{
  shareCode?: string;
}>();

const origin = window.location.origin;
const message = useMessage();
const terminalSettingsStore = useTerminalSettingsStore();

const { t } = useI18n();
const { width, height } = useWindowSize();
const { fitAddon, createTerminal, terminalEvent, terminalTheme } = useTerminalCreate();

// const fitAddon = new FitAddon();
// const searchAddon = new SearchAddon();

const sessionId = ref<string>('');
const terminalId = ref<string>('');
const terminalSelectionText = ref<string>('');

const socket = ref<WebSocket | ''>('');
const terminalRef = ref<HTMLDivElement>();
const terminalInstance = ref<Terminal>();
const sessionInfo = ref<TerminalSessionInfo>();

watch(
  [width, height],
  ([_newWidth, _newHeight]) => {
    if (!terminalInstance.value || !fitAddon) return;
    nextTick(() => {
      fitAddon.fit();
    });
  },
  { immediate: false }
);

const getXTerminalLineContent = (index: number) => {
  const buffer = terminalInstance.value?.buffer.active;

  if (!buffer) {
    return '';
  }

  const result: string[] = [];
  const bufferLineCount = buffer.length;

  let startLine = bufferLineCount;

  while (result.length < index || startLine >= 0) {
    startLine--;

    if (startLine < 0) {
      break;
    }

    const line = buffer.getLine(startLine);

    if (!line) {
      console.warn(`Line ${startLine} is empty or undefined`);
      continue;
    }

    result.unshift(line.translateToString());
  }

  return result.join('\n');
};

const createSocket = () => {
  const url = generateWsURL();

  const { ws } = useWebSocket(url, {
    protocols: ['JMS-KOKO'],
    autoReconnect: {
      retries: 5,
      delay: 3000,
    },
  });

  if (ws.value) {
    return ws.value;
  }

  message.error('Failed to create WebSocket connection');
  return '';
};
const mouseleave = () => {
  if (!terminalInstance.value) {
    return message.error('Terminal instance is not initialized');
  }

  terminalInstance.value.blur();
  lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.TERMINAL_CONTENT_RESPONSE, {
    content: getXTerminalLineContent(10),
    sessionId: sessionId.value,
    terminalId: terminalId.value,
  });
};
const handleDocumentClick = () => {
  if (!terminalInstance.value) {
    return message.error('Terminal instance is not initialized');
  }

  lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.CLICK, '');
};

onMounted(() => {
  socket.value = createSocket();

  if (!socket.value) {
    return;
  }

  const { initializeSocketEvent, setShareCode, eventBus } = useTerminalConnection();

  eventBus.on('luna-event', ({ event, data }) => {
    switch (event) {
      case LUNA_MESSAGE_TYPE.CLOSE:
      case LUNA_MESSAGE_TYPE.TERMINAL_ERROR:
        lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.CLOSE, data);
        break;
      case LUNA_MESSAGE_TYPE.OPEN:
        break;
      case LUNA_MESSAGE_TYPE.SHARE_CODE_RESPONSE:
        lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.SHARE_CODE_RESPONSE, data);
        break;
      default:
        lunaCommunicator.sendLuna(event as LunaEventType, data);
    }
  });

  eventBus.on('terminal-session', (info: TerminalSessionInfo) => {
    sessionId.value = info.session.id;
    sessionInfo.value = info;

    if (info.themeName) {
      const theme = terminalTheme(info.themeName);

      nextTick(() => {
        terminalInstance.value!.options.theme = theme;
      });
    }
  });

  eventBus.on('terminal-connect', ({ id }) => {
    terminalId.value = id;
  });

  terminalInstance.value = createTerminal();

  if (terminalRef.value) {
    terminalRef.value.addEventListener('mouseenter', () => {
      fitAddon.fit();
      terminalInstance.value?.focus();
    });
    terminalRef.value.addEventListener('contextmenu', async (e: MouseEvent) => {
      if (e.ctrlKey || terminalSettingsStore.quickPaste !== '1') return;

      e.preventDefault();

      let text: string = '';

      try {
        text = await readText();
      } catch (e) {
        if (terminalSelectionText.value) {
          console.error(e);
          text = terminalSelectionText.value;
        }
      }
      if (!text) {
        return;
      }

      if (socket.value === '' || socket.value.readyState === WebSocket.CLOSED) {
        message.error('WebSocket connection is closed, please refresh the page');
        return;
      }

      socket.value.send(formatMessage(terminalId.value, FORMATTER_MESSAGE_TYPE.TERMINAL_DATA, text));
    });

    terminalEvent(socket.value);

    terminalInstance.value.open(terminalRef.value);
  }

  if (props.shareCode) {
    setShareCode(props.shareCode);
  }

  initializeSocketEvent(terminalInstance.value, socket.value, t);

  const handLunaCommand = (msg: LunaMessage) => {
    if (!socket.value) {
      message.error('WebSocket connection may be closed, please refresh the page');
      return;
    }
    socket.value?.send(formatMessage(terminalId.value, FORMATTER_MESSAGE_TYPE.TERMINAL_DATA, msg.data));
  };
  const handLunaFocus = (_msg: LunaMessage) => {
    terminalInstance.value?.focus();
  };
  const handLunaThemeChange = (_msg: LunaMessage) => {
    const themeName = _msg.theme || 'Default';
    const theme = terminalTheme(themeName);

    nextTick(() => {
      terminalInstance.value!.options.theme = theme;
    });
  };

  // const handleCreateShareUrl = (shareLinkRequest: ShareUserRequest) => {
  //   if (!socket.value) {
  //     message.error('WebSocket connection is not established');
  //     return;
  //   }
  //   const data = shareLinkRequest.data.requestData || {};
  //   const perm = data.action_permission || data.action_perm || 'writable';
  //   socket.value.send(
  //     formatMessage(
  //       terminalId.value,
  //       FORMATTER_MESSAGE_TYPE.TERMINAL_SHARE,
  //       JSON.stringify({
  //         origin,
  //         session: shareLinkRequest.data.sessionId,
  //         users: data.users,
  //         expired_time: data.expired_time,
  //         action_permission: perm,
  //       })
  //     )
  //   );
  // };
  // const handleRemoveShareUser = (msg: LunaMessage) => {
  //   if (!socket.value) {
  //     message.error('WebSocket connection is not established');
  //     return;
  //   }
  //   if (!msg.data) {
  //     message.error('Invalid data for removing share user');
  //     return;
  //   }
  //   socket.value.send(
  //     formatMessage(
  //       terminalId.value,
  //       FORMATTER_MESSAGE_TYPE.TERMINAL_SHARE_USER_REMOVE,
  //       JSON.stringify({
  //         session: sessionId.value,
  //         user_meta: msg.data || {},
  //       })
  //     )
  //   );
  // };
  const handTerminalContent = (_msg: LunaMessage) => {
    if (!terminalInstance.value) {
      return message.error('Terminal instance is not initialized');
    }

    const content = getXTerminalLineContent(10);

    const data = {
      content,
      sessionId: sessionId.value,
      terminalId: terminalId.value,
    };

    lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.TERMINAL_CONTENT_RESPONSE, data);
  };

  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.CMD, handLunaCommand);
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.FOCUS, handLunaFocus);
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.TERMINAL_THEME_CHANGE, handLunaThemeChange);
  // lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.SHARE_CODE_REQUEST, handleCreateShareUrl);
  // lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.SHARE_USER_REMOVE, handleRemoveShareUser);
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.TERMINAL_CONTENT, handTerminalContent);

  // 添加事件监听
  document.addEventListener('click', handleDocumentClick);
});

onUnmounted(() => {
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.CMD);
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.FOCUS);
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.TERMINAL_THEME_CHANGE);
  // lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.SHARE_CODE_REQUEST);
  // lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.SHARE_USER_REMOVE);
  document.removeEventListener('click', handleDocumentClick);
});
</script>

<template>
  <div id="terminal-container" ref="terminalRef" class="w-screen h-screen" @mouseleave="mouseleave" />
</template>

<style scoped lang="scss">
#terminal-container {
  :deep(.terminal) {
    height: 100%;
    padding: 10px 0 5px 10px;
  }

  :deep(.xterm-viewport) {
    &::-webkit-scrollbar {
      height: 4px;
      width: 7px;
    }

    &::-webkit-scrollbar-thumb {
      background: rgba(255, 255, 255, 0.35);
      border-radius: 3px;
    }

    &::-webkit-scrollbar-thumb:hover {
      background: rgba(255, 255, 255, 0.5);
    }
  }
}
</style>
