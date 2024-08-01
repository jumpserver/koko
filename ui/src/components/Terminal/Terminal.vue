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
import { FitAddon } from '@xterm/addon-fit';
import { useLogger } from '@/hooks/useLogger.ts';
import { useMessage } from 'naive-ui';
import { useTerminal } from '@/hooks/useTerminal.ts';
import { useWebSocket } from '@/hooks/useWebSocket.ts';
import { onMounted, onUnmounted, ref } from 'vue';

import type { ILunaConfig } from '@/hooks/interface';
import type { ITerminalProps } from '@/hooks/interface';

import {
  handleEventFromLuna,
  sendEventToLuna,
  wsIsActivated,
  formatMessage,
  updateIcon
} from '@/components/Terminal/helper';
import ZmodemBrowser, {
  SentryConfig,
  Detection,
  ZmodemSession
} from 'nora-zmodemjs/src/zmodem_browser';
import mittBus from '@/utils/mittBus.ts';

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

const {
  getLunaConfig,
  createZsentry,
  createTerminal,
  preprocessInput,
  handleContextMenu,
  setTerminalTheme,
  handleCustomKeyEvent
} = useTerminal();
const { t } = useI18n();
const { createWebSocket } = useWebSocket();

const message = useMessage();

const lunaId = ref<string>('');
const origin = ref<string>('');
const terminalId = ref<string>('');
const currentUser = ref<string>('');

const setting = ref<any>(null);
const zmodemStatus = ref<boolean>(false);
const lastSendTime = ref<Date>(new Date());

let ws: WebSocket;
let zsentry: ZmodemBrowser.Sentry;
let fitAddonInstance = ref<FitAddon | null>(null);
let terminalInstance = ref<Terminal | null>(null);
// let zmodeSession: ZmodemSession;
// const zmodeDialogVisible = ref(false);

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
const writeBufferToTerminal = (data: any, terminal: Terminal) => {
  if (!props.enableZmodem && zmodemStatus.value)
    return debug('未开启 Zmodem 且当前在 Zmodem 状态, 不允许显示');

  terminal.write(new Uint8Array(data));
};

const dispatch = (data: any, terminal: Terminal) => {
  if (data === undefined) return;

  let msg = JSON.parse(data);

  debug('dispatchData', msg);

  switch (msg.type) {
    case 'CONNECT':
      terminalId.value = msg.id;

      try {
        fitAddonInstance.value?.fit();
      } catch (e) {
        console.log(e);
      }

      const terminalData = {
        cols: terminal.cols,
        rows: terminal.rows,
        code: props.shareCode
      };

      const info = JSON.parse(msg.data);

      debug('dispatchInfo', info);

      currentUser.value = info.user;
      setting.value = info.setting;

      updateIcon(setting.value);

      ws.send(formatMessage(terminalId.value, 'TERMINAL_INIT', JSON.stringify(terminalData)));
      break;
    case 'CLOSE':
      terminal.writeln('Receive Connection closed');
      ws.close();
      sendEventToLuna('CLOSE', '');
      break;
    case 'PING':
      break;
    case 'TERMINAL_ACTION':
      break;
    case 'TERMINAL_ERROR':
    case 'ERROR':
      message.error(msg.err);
      terminal.writeln(msg.err);
      break;
    case 'MESSAGE_NOTIFY':
      const errMsg = msg.err;
      const eventData = JSON.parse(msg.data);

      const eventName = eventData.event_name;

      switch (eventName) {
        case 'sync_user_preference':
          if (errMsg === '' || errMsg === null) {
            const successNotify = t('SyncUserPreferenceSuccess');
            message.success(successNotify);
          } else {
            const errNotify = `${t('SyncUserPreferenceFailed')}: ${errMsg}`;
            message.error(errNotify);
          }
          break;
        default:
          debug('unknown: ', eventName);
      }
      break;
    default:
      debug(`Default: ${data}`);
  }

  emits('wsData', msg.type, msg, terminal, setting.value);
};
const getTerminal = (): Terminal => {
  const config: ILunaConfig = getLunaConfig();
  const el = document.getElementById('terminal') as HTMLElement;

  const { fitAddon, term } = createTerminal(el, config);

  terminalInstance.value = term;
  fitAddonInstance.value = fitAddon;

  term.attachCustomKeyEventHandler((event: KeyboardEvent) =>
    handleCustomKeyEvent(event, lunaId.value, origin.value)
  );
  el.addEventListener(
    'contextmenu',
    ($event: MouseEvent) => {
      const text = handleContextMenu($event);

      if (wsIsActivated(ws)) {
        ws.send(formatMessage(terminalId.value, 'TERMINAL_DATA', text));
      }
    },
    false
  );

  return term;
};
const connect = async () => {
  debug(props.connectURL);

  const terminal: Terminal = getTerminal();

  debug(ZmodemBrowser);

  const config: SentryConfig = {
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
  zsentry = createZsentry(config);

  terminal.onData(data => {
    if (!wsIsActivated(ws)) return debug('WebSocket Closed');

    if (!props.enableZmodem && zmodemStatus.value)
      return debug('未开启 Zmodem 且当前在 Zmodem 状态，不允许输入');

    lastSendTime.value = new Date();
    debug('Term on data event');

    data = preprocessInput(data);
    sendEventToLuna('KEYBOARDEVENT', '');

    ws.send(formatMessage(terminalId.value, 'TERMINAL_DATA', data));
  });
  terminal.onResize(({ cols, rows }) => {
    if (!wsIsActivated(ws)) return;

    debug('Send Term Resize');

    ws.send(formatMessage(terminalId.value, 'TERMINAL_RESIZE', JSON.stringify({ cols, rows })));
  });

  ws = createWebSocket(props.connectURL, terminal, lastSendTime);
  ws.onmessage = (e: MessageEvent) => {
    lastSendTime.value = new Date();

    if (typeof e.data === 'object') {
      if (props.enableZmodem) {
        zsentry.consume(e.data);
      } else {
        writeBufferToTerminal(e.data, terminal);
      }
    } else {
      debug(typeof e.data);
      dispatch(e.data, terminal);
    }
  };

  window.SendTerminalData = sendDataFromWindow;

  window.Reconnect = () => {
    emits('event', 'reconnect', '');
  };
};

onMounted(() => {
  // 监听从 Luna 发送的事件
  window.addEventListener(
    'message',
    (e: MessageEvent) =>
      handleEventFromLuna(e, emits, lunaId, origin, terminalInstance, sendDataFromWindow),
    false
  );

  // 发起连接
  connect();

  // 设置主题
  setTerminalTheme(props.themeName, terminalInstance.value as Terminal, emits);

  // 修改主题
  mittBus.on('set-theme', ({ themeName }) => {
    setTerminalTheme(themeName as string, terminalInstance.value as Terminal);
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
