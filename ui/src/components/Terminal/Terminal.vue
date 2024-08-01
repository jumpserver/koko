<template>
  <n-layout style="height: 100vh">
    <n-scrollbar trigger="hover" style="max-height: 880px">
      <div id="terminal" class="terminal-container"></div>
    </n-scrollbar>
  </n-layout>
</template>

<script setup lang="ts">
import { Terminal } from '@xterm/xterm';
import { useLogger } from '@/hooks/useLogger.ts';
import { useTerminal } from '@/hooks/useTerminal.ts';
import { useWebSocket } from '@/hooks/useWebSocket.ts';
import { onMounted, onUnmounted, Ref, ref } from 'vue';

import type { ILunaConfig } from '@/hooks/interface';
import type { ITerminalProps } from '@/hooks/interface';

import { handleEventFromLuna, wsIsActivated, formatMessage } from '@/components/Terminal/helper';
import ZmodemBrowser, {
  SentryConfig,
  Detection,
  ZmodemSession
} from 'nora-zmodemjs/src/zmodem_browser';
import mittBus from '@/utils/mittBus.ts';
import { useZsentry } from '@/hooks/useZsentry.ts';

const { debug, info } = useLogger('TerminalComponent');

const props = withDefaults(defineProps<ITerminalProps>(), {
  themeName: 'Default',
  enableZmodem: false
});

const emits = defineEmits<{
  (e: 'wsData', msgType: string, msg: any, terminal: Terminal, setting: any): void;
  (e: 'event', event: string, data: string): void;
  (e: 'background-color', backgroundColor: string): void;
}>();

const { createTerminal, setTerminalTheme, handleTerminalOnResize, handleTerminalOnData } =
  useTerminal();
const { createZsentry } = useZsentry();
const { createWebSocket, onWebsocketOpen, handleOnWebsocketMessage } = useWebSocket();

const lunaId = ref<string>('');
const origin = ref<string>('');
const terminalId = ref<string>('');
const currentUser = ref<string>('');

const setting = ref<any>(null);
const zmodemStatus = ref<boolean>(false);
const lastSendTime: Ref<Date> = ref(new Date());

let ws: WebSocket;
let zsentry: ZmodemBrowser.Sentry;
let term: Terminal;

let lunaConfig: ILunaConfig = {};

// let zmodeSession: ZmodemSession;
// const zmodeDialogVisible = ref(false);

const getLunaConfig = (): ILunaConfig => {
  let fontSize: number = 14;
  let quickPaste: string = '0';
  let backspaceAsCtrlH: string = '0';
  let localSettings: string | null = localStorage.getItem('LunaSetting');

  if (localSettings !== null) {
    let settings = JSON.parse(localSettings);
    let commandLine = settings['command_line'];
    if (commandLine) {
      fontSize = commandLine['character_terminal_font_size'];
      quickPaste = commandLine['is_right_click_quickly_paste'] ? '1' : '0';
      backspaceAsCtrlH = commandLine['is_backspace_as_ctrl_h'] ? '1' : '0';
    }
  }

  if (!fontSize || fontSize < 5 || fontSize > 50) {
    fontSize = 13;
  }

  lunaConfig['fontSize'] = fontSize;
  lunaConfig['quickPaste'] = quickPaste;
  lunaConfig['backspaceAsCtrlH'] = backspaceAsCtrlH;
  lunaConfig['ctrlCAsCtrlZ'] = '0';

  // 根据用户的操作系统类型设置行高
  const ua: string = navigator.userAgent.toLowerCase();
  lunaConfig['lineHeight'] = ua.indexOf('windows') !== -1 ? 1.2 : 1;

  return lunaConfig;
};
const getzsentryConfig = (terminal: Terminal): SentryConfig => {
  return {
    // 将数据写入终端。
    to_terminal: (octets: string) => {
      if (zsentry && !zsentry.get_confirmed_session()) {
        terminal.write(octets);
      }
    },
    // 将数据通过 WebSocket 发送
    sender: (octets: Uint8Array) => {
      if (!wsIsActivated(ws)) {
        debug('WebSocket Closed');
        return;
      }
      lastSendTime.value = new Date();
      debug(`octets: ${octets}`);
      ws.send(new Uint8Array(octets));
    },
    // 处理 Zmodem 撤回事件
    on_retract: () => {
      info('Zmodem Retract');
    },
    // 处理检测到的 Zmodem 会话
    on_detect: (detection: Detection) => {
      const zsession: ZmodemSession = detection.confirm();
      terminal.write('\r\n'); // 使用 terminal ref

      if (zsession.type === 'send') {
        handleSendSession(zsession);
      } else {
        handleReceiveSession(zsession);
      }
    }
  };
};

const handleCunstomWindowEvent = (terminal: Terminal) => {
  window.addEventListener(
    'message',
    (e: MessageEvent) =>
      handleEventFromLuna(e, emits, lunaId, origin, terminal, sendDataFromWindow),
    false
  );

  window.SendTerminalData = sendDataFromWindow;

  window.Reconnect = () => {
    emits('event', 'reconnect', '');
  };
};
const handleTerminalEvent = (terminal: Terminal) => {
  terminal.onData(data =>
    handleTerminalOnData(
      ws,
      data,
      terminalId.value,
      lunaConfig,
      props.enableZmodem,
      zmodemStatus.value,
      lastSendTime
    )
  );

  terminal.onResize(({ cols, rows }) => handleTerminalOnResize(ws, cols, rows, terminalId.value));
};

const handleSendSession = (zsession: ZmodemSession) => {
  console.log(zsession);
};
const handleReceiveSession = (zsession: ZmodemSession) => {
  console.log(zsession);
};

const sendWsMessage = (type: string, data: any) => {
  if (wsIsActivated(ws))
    return ws.send(formatMessage(terminalId.value, type, JSON.stringify(data)));
};
const sendDataFromWindow = (data: any): void => {
  if (!wsIsActivated(ws)) return debug('WebSocket Disconnected');

  if (props.enableZmodem && !zmodemStatus.value) {
    ws.send(formatMessage(terminalId.value, 'TERMINAL_DATA', data));
    debug('Send Data From Window');
  }
};

const connect = async () => {
  debug(props.connectURL);

  const el: HTMLElement = document.getElementById('terminal')!;

  // 创建 Terminl 实例
  const { terminal, fitAddon } = createTerminal(el, lunaConfig);

  // 获取 zsentry 配置
  const zsentryConfig: SentryConfig = getzsentryConfig(terminal);

  // 创建 zsentry 实例
  zsentry = createZsentry(zsentryConfig);

  // 处理需要 ws 的 Terminal 事件
  handleTerminalEvent(terminal);

  // 创建 WebSocket
  ws = createWebSocket(props.connectURL, terminal, lastSendTime);

  // 处理 message 事件
  ws.onopen = () => onWebsocketOpen(terminalId.value);
  ws.onmessage = (e: MessageEvent) => {
    handleOnWebsocketMessage(
      e,
      terminal,
      props.enableZmodem,
      zmodemStatus.value,
      lastSendTime,
      zsentry,
      terminalId,
      fitAddon,
      props.shareCode,
      currentUser,
      setting,
      emits
    );
  };

  handleCunstomWindowEvent(terminal);

  term = terminal;
};

onMounted(() => {
  // 获取 Luan 配置
  getLunaConfig();

  // 发起连接
  connect();

  // 设置主题
  setTerminalTheme(props.themeName, term, emits);

  // 修改主题
  mittBus.on('set-theme', ({ themeName }) => {
    setTerminalTheme(themeName as string, term);
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

    .xterm-rows {
      //padding: 10px 0 0 10px;
    }
  }
}
</style>
