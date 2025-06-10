<template>
  <div id="terminal-container" class="w-screen h-screen"></div>
</template>

<script setup lang="ts">
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { SearchAddon } from '@xterm/addon-search';
import xtermTheme from 'xterm-theme';
import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { onMounted, onUnmounted, ref, nextTick, watch } from 'vue';
import { useDebounceFn, useWebSocket } from '@vueuse/core';
import { writeText, readText } from 'clipboard-polyfill';

import { LUNA_MESSAGE_TYPE, FORMATTER_MESSAGE_TYPE } from '@/types/modules/message.type';
import { defaultTheme } from '@/utils/config';
import { lunaCommunicator, LunaEventType } from '@/utils/lunaBus';
import { formatMessage } from '@/utils';
import { generateWsURL } from '@/hooks/helper';
import { useTerminalConnection } from '@/hooks/useTerminalConnection';
import { LunaMessage, ShareUserRequest, TerminalSessionInfo } from '@/types/modules/postmessage.type';
import { getDefaultTerminalConfig } from '@/utils/guard';
import { useWindowSize } from '@vueuse/core'

const { width, height } = useWindowSize()
const { t } = useI18n();
const message = useMessage();

const props = defineProps<{
  shareCode?: string;
}>();

const fitAddon = new FitAddon();
const searchAddon = new SearchAddon();
const terminalId = ref<string>('');
const terminalSelectionText = ref<string>('');
const terminalInstance = ref<Terminal>();
const sessionId = ref<string>('');
const origin = window.location.origin;
const sessionInfo = ref<TerminalSessionInfo>();

const debouncedSendLunaKey = useDebounceFn((key: string) => {
  switch (key) {
    case 'ArrowRight':
      lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.KEYEVENT, 'alt+shift+right');
      break;
    case 'ArrowLeft':
      lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.KEYEVENT, 'alt+shift+left');
      break;
  }
}, 500);


const socket = ref<WebSocket | ''>('');

const setupTerminalEvent = (terminal: Terminal) => {
  terminal.onSelectionChange(async () => {
    terminalSelectionText.value = terminal.getSelection().trim();

    if (!terminalSelectionText.value) {
      return;
    }

    await writeText(terminalSelectionText.value);
  });
};
/**
 * @description 创建 WebSocket 连接
 * @returns {WebSocket | ''}
 */
const createSocket = (): WebSocket | '' => {
  const url = generateWsURL();

  const { ws } = useWebSocket(url, {
    protocols: ['JMS-KOKO'],
    autoReconnect: {
      retries: 5,
      delay: 3000
    }
  });

  if (ws.value) {
    return ws.value;
  }
  message.error('Failed to create WebSocket connection');
  return '';
};

const getXtermTheme = (themeName: string) => {
  if (!xtermTheme[themeName]) {
    return defaultTheme;
  }
  return xtermTheme[themeName];
};

const defaultTerminalCfg = ref(getDefaultTerminalConfig());

const createXtermInstance = () => {
  const xterminal = new Terminal({
    allowProposedApi: true,
    rightClickSelectsWord: true,
    scrollback: 5000,
    theme: getXtermTheme(defaultTerminalCfg.value.themeName),
    fontSize: defaultTerminalCfg.value.fontSize,
    lineHeight: defaultTerminalCfg.value.lineHeight,
    fontFamily: defaultTerminalCfg.value.fontFamily
  });
  xterminal.loadAddon(fitAddon);
  xterminal.loadAddon(searchAddon);
  return xterminal;
};

watch([width, height], ([_newWidth, _newHeight]) => {
  if (!terminalInstance.value || !fitAddon) return;
  nextTick(() => {
    // 调整终端大小
    fitAddon.fit();
  });
}, { immediate: false }
);

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
      case LUNA_MESSAGE_TYPE.SHARE_CODE_RESPONSE:
        lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.SHARE_CODE_RESPONSE, data);
        console.log('Received share code response:', data);
        break;
      default:
        lunaCommunicator.sendLuna(event as LunaEventType, data);
    }
  });
  eventBus.on('terminal-session', (info: TerminalSessionInfo) => {
    sessionId.value = info.session.id;
    sessionInfo.value = info;
    if (info.themeName) {
      const theme = getXtermTheme(info.themeName);
      nextTick(() => {
        terminalInstance.value!.options.theme = theme;
      });
    }
    if (info.backspaceAsCtrlH) {
      defaultTerminalCfg.value.backspaceAsCtrlH = info.backspaceAsCtrlH ? "1" : "0";
    }


    console.log('Received terminal session info:', info);
  });

  eventBus.on('terminal-connect', ({ id }) => {
    terminalId.value = id;
  });

  terminalInstance.value = createXtermInstance();

  const terminalContainer = document.getElementById('terminal-container');
  if (!terminalContainer) {
    message.error('Terminal container not found');
    return;
  }
  terminalContainer.addEventListener('mouseenter', () => {
    fitAddon.fit();
    terminalInstance.value?.focus();
  });

  terminalContainer.addEventListener(
    'contextmenu',
    async (e: MouseEvent) => {
      if (e.ctrlKey || defaultTerminalCfg.value.quickPaste !== '1') return;
      e.preventDefault();
      let text: string = '';
      try {
        text = await readText();
      } catch (_e) {
        if (terminalSelectionText.value) {
          text = terminalSelectionText.value;
        }
      }
      if (!text) {
        console.log('No text to paste');
        return;
      }

      if (socket.value === '' || socket.value.readyState === WebSocket.CLOSED) {
        message.error('WebSocket connection is closed, please refresh the page');
        return;
      }
      socket.value.send(formatMessage(terminalId.value, FORMATTER_MESSAGE_TYPE.TERMINAL_DATA, text));
    },
    false
  );
  terminalInstance.value.attachCustomKeyEventHandler((e: KeyboardEvent) => {
    if (e.altKey && e.shiftKey && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
      debouncedSendLunaKey(e.key);
      return false;
    }
    if (!terminalInstance.value) {
      message.error('Terminal instance is not initialized');
      return false;
    }
    if (e.ctrlKey && e.key === 'c' && terminalInstance.value.hasSelection()) {
      return false;
    }

    return !(e.ctrlKey && e.key === 'v');
  });
  terminalInstance.value.open(terminalContainer);

  const getXTerminalLineContent = (index: number) => {
    const buffer = terminalInstance.value?.buffer.active;
    if (!buffer) {
      return '';
    }
    const result: string[] = [];
    const bufferLineCount = buffer.length;
    let startLine = bufferLineCount;
    while ((result.length < index) || startLine >= 0) {
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


  if (props.shareCode) {
    setShareCode(props.shareCode);
  }
  setupTerminalEvent(terminalInstance.value);
  initializeSocketEvent(terminalInstance.value, socket.value, t);

  const debouncedReisze = useDebounceFn(({ cols, rows }) => {
    fitAddon.fit();
    if (!socket.value) {
      message.error('WebSocket connection may be closed, please refresh the page');
      return;
    }
    const resizeData = JSON.stringify({ cols, rows });
    socket.value?.send(formatMessage(terminalId.value, FORMATTER_MESSAGE_TYPE.TERMINAL_RESIZE, resizeData));
  }, 200);

  terminalInstance.value.onResize(({ cols, rows }) => debouncedReisze({ cols, rows }));

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
    const theme = getXtermTheme(themeName);

    nextTick(() => {
      terminalInstance.value!.options.theme = theme;
    });
  };

  const handleCreateShareUrl = (shareLinkRequest: ShareUserRequest) => {
    console.log('Received share code request:', shareLinkRequest);

    if (!socket.value) {
      message.error('WebSocket connection is not established');
      return;
    }
    const data = shareLinkRequest.data || {};
    const perm = data.action_permission || data.action_perm || 'writable';
    socket.value.send(
      formatMessage(
        terminalId.value,
        FORMATTER_MESSAGE_TYPE.TERMINAL_SHARE,
        JSON.stringify({
          origin: origin,
          session: sessionId.value,
          users: data.users,
          expired_time: data.expired_time,
          action_permission: perm
        })
      )
    );
  };
  const handleRemoveShareUser = (msg: LunaMessage) => {
    console.log('Received remove share user request:', msg);
    if (!socket.value) {
      message.error('WebSocket connection is not established');
      return;
    }
    if (!msg.data) {
      message.error('Invalid data for removing share user');
      return;
    }
    socket.value.send(
      formatMessage(
        terminalId.value,
        FORMATTER_MESSAGE_TYPE.TERMINAL_SHARE_USER_REMOVE,
        JSON.stringify({
          session: sessionId.value,
          user_meta: msg.data || {}
        })
      )
    );
  };

  const handTerminalContent = (_msg: LunaMessage) => {
    if (!terminalInstance.value) {
      message.error('Terminal instance is not initialized');
      return;
    }
    const content = getXTerminalLineContent(10);
    const data = {
      content: content,
      sessionId: sessionId.value,
      terminalId: terminalId.value
    };
    lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.TERMINAL_CONTENT_RESPONSE, data);
  };

  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.CMD, handLunaCommand);
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.FOCUS, handLunaFocus);
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.TERMINAL_THEME_CHANGE, handLunaThemeChange);
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.SHARE_CODE_REQUEST, handleCreateShareUrl);
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.SHARE_USER_REMOVE, handleRemoveShareUser);
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.TERMINAL_CONTENT, handTerminalContent)
})


onUnmounted(() => {
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.CMD);
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.FOCUS);
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.TERMINAL_THEME_CHANGE);
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.SHARE_CODE_REQUEST);
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.SHARE_USER_REMOVE);
});

</script>

<style scoped lang="scss">
#terminal-container {
  :deep(.terminal) {
    height: 100%;
    padding: 10px 0 5px 10px;
  }

  :deep(.xterm-viewport) {
    background-color: #000000;

    &::-webkit-scrollbar {
      height: 4px;
      width: 7px;
    }

    &::-webkit-scrollbar-track {
      background: #000000;
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
