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
import { onMounted, onUnmounted, ref, nextTick } from 'vue';
import { useWebSocket } from '@vueuse/core';
import { v4 as uuid } from 'uuid';
import { writeText, readText } from 'clipboard-polyfill';

import { LUNA_MESSAGE_TYPE, FORMATTER_MESSAGE_TYPE } from '@/types/modules/message.type';
import { defaultTheme } from '@/utils/config';
import { lunaCommunicator } from '@/utils/lunaBus';
import { formatMessage } from '@/utils';
import { generateWsURL } from '@/hooks/helper';
import { useTerminalConnection } from '@/hooks/useTerminalConnection';
import { LunaMessage, ShareUserRequest, TerminalSessionInfo} from '@/types/modules/postmessage.type';
import { getDefaultTerminalConfig } from '@/utils/guard';


const props = defineProps<{
  shareCode?: string;
}>();

const fitAddon = new FitAddon();
const searchAddon = new SearchAddon();
const terminalId = uuid();
const terminalSelectionText = ref<string>('');
const terminalInstance = ref<Terminal>();
const sessionId = ref<string>('');
const origin = window.location.origin;
const sessionInfo = ref<TerminalSessionInfo>();

const { t } = useI18n();
const message = useMessage();

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
const defaultTerminalCfg = getDefaultTerminalConfig();

const createXtermInstance = () => {
  const xterminal = new Terminal({
    allowProposedApi: true,
    rightClickSelectsWord: true,
    scrollback: 5000,
    theme: getXtermTheme(defaultTerminalCfg.themeName),
    fontSize: defaultTerminalCfg.fontSize,
    lineHeight: defaultTerminalCfg.lineHeight,
    fontFamily: defaultTerminalCfg.fontFamily
  });
  xterminal.loadAddon(fitAddon);
  xterminal.loadAddon(searchAddon);
  return xterminal;
};





onMounted(() => {

  socket.value = createSocket();
  if (!socket.value) {
    return;
  }
  const { initializeSocketEvent, setShareCode, terminalResizeEvent, eventBus } = useTerminalConnection();
  eventBus.on('luna-event', ({event, data}) => {
    switch (event) {
      case LUNA_MESSAGE_TYPE.CLOSE,LUNA_MESSAGE_TYPE.TERMINAL_ERROR:
        lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.CLOSE, data);
        break;
      case LUNA_MESSAGE_TYPE.SHARE_CODE_RESPONSE:
        lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.SHARE_CODE_RESPONSE, data);
        console.log('Received share code response:', data);
        break;
      default:
        console.warn(`Unknown event type: ${event}`);
    }
  });
  eventBus.on('terminal-session', (info: TerminalSessionInfo) => {
    sessionId.value = info.session.id;
    sessionInfo.value = info;
    console.log('Received terminal session info:', info);
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
      if (e.ctrlKey || defaultTerminalCfg.quickPaste !== '1') return;

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

      if (!socket.value) {
        message.error('WebSocket connection is not established');
        return;
      }
      socket.value.send(formatMessage(terminalId, FORMATTER_MESSAGE_TYPE.TERMINAL_DATA, text));
    },
    false
  );
  terminalInstance.value.open(terminalContainer);

  if (props.shareCode) {
    setShareCode(props.shareCode);
  }
  setupTerminalEvent(terminalInstance.value);
  initializeSocketEvent(terminalInstance.value, socket.value, t);
  terminalResizeEvent(terminalInstance.value, socket.value, fitAddon);

  const handLunaCommand = (msg: LunaMessage) => {
    if (!socket.value) {
      message.error('WebSocket connection may be closed, please refresh the page');
      return;
    }
    socket.value?.send(formatMessage(terminalId, FORMATTER_MESSAGE_TYPE.TERMINAL_DATA, msg.data));

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
        terminalId,
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


  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.CMD, handLunaCommand);
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.FOCUS, handLunaFocus);
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.TERMINAL_THEME_CHANGE, handLunaThemeChange);
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.SHARE_CODE_REQUEST, handleCreateShareUrl);
  console.log('Luna communicator initialized and event listeners set up');
})


onUnmounted(() => {
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.CMD);
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.FOCUS);
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.TERMINAL_THEME_CHANGE);
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.SHARE_CODE_REQUEST);
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
