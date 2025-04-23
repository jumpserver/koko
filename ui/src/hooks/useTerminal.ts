// Terminal 相关
import xtermTheme from 'xterm-theme';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
// import { WebglAddon } from '@xterm/addon-webgl';
import { ISearchOptions, SearchAddon } from '@xterm/addon-search';
import { Sentry } from 'nora-zmodemjs/src/zmodem_browser';
import { defaultTheme } from '@/config';

// hook
import { watch, watchEffect } from 'vue';
import { createDiscreteApi } from 'naive-ui';
import { useWebSocket } from '@vueuse/core';
import { useSentry } from '@/hooks/useZsentry.ts';

// store
import { storeToRefs } from 'pinia';
import { useTerminalStore } from '@/store/modules/terminal.ts';
import { useParamsStore } from '@/store/modules/params.ts';

import { onUnmounted, ref, Ref } from 'vue';
import { writeBufferToTerminal } from '@/utils';
import type { ILunaConfig } from '@/hooks/interface';

// 工具函数
import {
  generateWsURL,
  handleContextMenu,
  handleCustomKey,
  handleTerminalOnData,
  handleTerminalResize,
  handleTerminalSelection,
  onWebsocketOpen,
  onWebsocketWrong
} from '@/hooks/helper';
import {
  formatMessage,
  sendEventToLuna,
  updateIcon,
  wsIsActivated
} from '@/components/TerminalComponent/helper';
import mittBus from '@/utils/mittBus.ts';

enum MessageType {
  PING = 'PING',
  CLOSE = 'CLOSE',
  ERROR = 'ERROR',
  CONNECT = 'CONNECT',
  TERMINAL_ERROR = 'TERMINAL_ERROR',
  MESSAGE_NOTIFY = 'MESSAGE_NOTIFY',
  TERMINAL_ACTION = 'TERMINAL_ACTION',
  TERMINAL_SHARE_USER_REMOVE = 'TERMINAL_SHARE_USER_REMOVE'
}

interface ITerminalInstance {
  terminal: Terminal | undefined;
  setTerminalTheme: (themeName: string, terminal: Terminal, emits: any) => void;
}

interface ICallbackOptions {
  // terminal 类型
  type: string;

  // 传递进来的 socket，不传则在 createTerminal 时创建
  transSocket?: WebSocket;

  // emit 事件
  emitCallback?: (
    e: string,
    type: string,
    msg: any,
    terminal?: Terminal
  ) => void;

  // t
  i18nCallBack?: (key: string) => string;
}

const { message } = createDiscreteApi(['message']);

export const useTerminal = async (
  el: HTMLElement,
  option: ICallbackOptions
): Promise<ITerminalInstance> => {
  let sentry: Sentry;
  let socket: WebSocket;
  let terminal: Terminal | undefined;
  let lunaConfig: ILunaConfig;

  let fitAddon: FitAddon = new FitAddon();
  let searchAddon: SearchAddon = new SearchAddon();

  let type: string = option.type;

  let counter = ref(0);
  let origin = ref('');
  let lunaId = ref('');
  let terminalId = ref('');
  let termSelectionText = ref('');
  let pingInterval = ref<number | null>(null);
  let guaranteeInterval = ref<number | null>(null);

  let lastSendTime: Ref<Date> = ref(new Date());
  let lastReceiveTime: Ref<Date> = ref(new Date());

  let handleSocketMessage: any;

  watch(
    () => counter.value,
    counter => {
      if (counter >= 5) {
        clearInterval(guaranteeInterval.value!);

        socket.close();
        terminal?.write('\r\n\r\n\r\n\x1b[31mFailed to connect to Luna\x1b[0m');

        alert('Failed to connect to Luna');
        window.close();
      }
    }
  );

  const dispatch = (data: string) => {
    if (!data) return;

    let msg = JSON.parse(data);

    const terminalStore = useTerminalStore();
    const paramsStore = useParamsStore();

    const { enableZmodem, zmodemStatus } = storeToRefs(terminalStore);

    switch (msg.type) {
      case MessageType.CONNECT: {
        fitAddon.fit();
        terminalId.value = msg.id;

        const terminalData = {
          cols: terminal && terminal.cols,
          rows: terminal && terminal.rows,
          code: paramsStore.shareCode
        };

        const info = JSON.parse(msg.data);

        paramsStore.setSetting(info.setting);
        paramsStore.setCurrentUser(info.user);

        updateIcon(info.setting);

        socket.send(
          formatMessage(
            terminalId.value,
            'TERMINAL_INIT',
            JSON.stringify(terminalData)
          )
        );
        break;
      }
      case MessageType.CLOSE: {
        socket.close();
        sendEventToLuna('CLOSE', '', lunaId.value, origin.value);
        break;
      }
      case MessageType.PING:
        break;
      case MessageType.TERMINAL_ACTION: {
        const action = msg.data;

        switch (action) {
          case 'ZMODEM_START': {
            terminalStore.setTerminalConfig('zmodemStatus', true);
            break;
          }
          case 'ZMODEM_END': {
            if (!enableZmodem.value && zmodemStatus.value) {
              terminal?.write('\r\n');
              terminalStore.setTerminalConfig('zmodemStatus', false);
            }
            break;
          }
          default: {
            terminalStore.setTerminalConfig('zmodemStatus', false);
          }
        }
        break;
      }
      case MessageType.TERMINAL_ERROR:
      case MessageType.ERROR: {
        terminal?.write(msg.err);
        sendEventToLuna('TERMINAL_ERROR', '', lunaId.value, origin.value);
        clearInterval(guaranteeInterval.value!);
        break;
      }
      case MessageType.MESSAGE_NOTIFY: {
        break;
      }
      case MessageType.TERMINAL_SHARE_USER_REMOVE: {
        option.i18nCallBack &&
          message.info(option.i18nCallBack('RemoveShareUser'));
        socket.close();
        break;
      }
      default: {
      }
    }

    option.emitCallback &&
      option.emitCallback('socketData', msg.type, msg, terminal);
  };

  /**
   * search Terminal 数据
   */
  const searchKeyWord = (keyword: string, type: string) => {
    const searchOption: ISearchOptions = {
      caseSensitive: false,
      // @ts-ignore
      decorations: {
        matchBackground: '#FFFF54',
        activeMatchBackground: '#F19B4A'
      }
    };

    if (type === 'next') {
      searchAddon.findNext(keyword, searchOption);
    } else {
      searchAddon.findPrevious(keyword, searchOption);
    }
  };

  /**
   * 设置主题
   */
  const setTerminalTheme = (
    themeName: string,
    terminal: Terminal,
    emits: any
  ) => {
    const theme = xtermTheme[themeName] || defaultTheme;

    terminal.options.theme = theme;

    emits('background-color', theme.background);
  };

  /**
   * 设置相关请求信息
   *
   * @param type
   * @param data
   */
  const sendWsMessage = (type: string, data: any) => {
    socket?.send(formatMessage(terminalId.value, type, JSON.stringify(data)));
  };

  /**
   * 处理非 K8s 的 message 事件
   */
  const handleMessage = (event: MessageEvent) => {
    lastReceiveTime.value = new Date();

    const terminalStore = useTerminalStore();
    const { enableZmodem, zmodemStatus } = storeToRefs(terminalStore);

    if (typeof event.data === 'object') {
      if (enableZmodem.value) {
        try {
          sentry.consume(event.data);
        } catch (e) {
          if (sentry.get_confirmed_session()) {
            sentry.get_confirmed_session()?.abort();
            message.error('File transfer error, file transfer interrupted');
          }
        }
      } else {
        writeBufferToTerminal(
          enableZmodem.value,
          zmodemStatus.value,
          terminal!,
          event.data
        );
      }
    } else {
      dispatch(event.data);
    }
  };

  /**
   * 发送 TERMINAL_DATA
   *
   * @param data
   */
  const sendDataFromWindow = (data: any) => {
    if (!wsIsActivated(socket)) {
      return message.error('WebSocket Disconnected');
    }

    const terminalStore = useTerminalStore();
    const { enableZmodem, zmodemStatus } = storeToRefs(terminalStore);

    if (enableZmodem.value && !zmodemStatus.value) {
      socket?.send(formatMessage(terminalId.value, 'TERMINAL_DATA', data));
    }
  };

  /**
   *  @description 保证连接是通过 Luna 发起的, 如果 ping 次数大于 5 次，则直接关闭连接
   */
  const guaranteeLunaConnection = () => {
    watchEffect(() => {
      if (!lunaId.value) {
        guaranteeInterval.value = setInterval(() => {
          counter.value++;

          console.log(
            '%c DEBUG [ Send Luna PING ]:',
            'font-size:13px; background: #1ab394; color:#fff;',
            counter.value
          );
        }, 1000);
      } else {
        clearInterval(guaranteeInterval.value!);
        sendEventToLuna('PING', '', lunaId.value, origin.value);
      }
    });
  };

  /**
   * 初始非 k8s 的 socket 事件
   */
  const initSocketEvent = () => {
    if (socket) {
      socket.onopen = () => {
        const excludePath = ['/koko/monitor/'];
        const currentPath = window.location.pathname;
        onWebsocketOpen(socket, lastSendTime.value, terminalId.value, pingInterval, lastReceiveTime);

        // 如果当前路径包含了 excludePath 中的任意一个，则不进行保证连接
        if (excludePath.some(path => currentPath.includes(path))) {
          return;
        }

        guaranteeLunaConnection();
      };
      socket.onmessage = (event: MessageEvent) => {
        handleMessage(event);
      };
      socket.onerror = (event: Event) => {
        onWebsocketWrong(event, 'error', terminal);
      };
      socket.onclose = (event: CloseEvent) => {
        onWebsocketWrong(event, 'disconnected', terminal);
      };
    }
  };

  /**
   * 初始化 El 节点相关事件
   */
  const initElEvent = () => {
    el.addEventListener(
      'mouseenter',
      () => {
        fitAddon.fit();
        terminal?.focus();
      },
      false
    );
    el.addEventListener(
      'contextmenu',
      (e: MouseEvent) => {
        handleContextMenu(
          e,
          lunaConfig,
          socket!,
          terminalId.value,
          termSelectionText.value
        );
      },
      false
    );
  };

  /**
   * 设置 window 自定义事件
   */
  const initCustomWindowEvent = () => {
    window.addEventListener('message', (e: MessageEvent) => {
      const message = e.data;

      switch (message.name) {
        case 'PING': {
          lunaId.value = message.id;
          origin.value = e.origin;

          sendEventToLuna('PONG', '', lunaId.value, origin.value);
          break;
        }
        case 'PONG': {
          lunaId.value = message.id;
          origin.value = e.origin;

          clearInterval(guaranteeInterval.value!);
          break;
        }
        case 'CMD': {
          sendDataFromWindow(message.data);
          break;
        }
        case 'FOCUS': {
          terminal?.focus();
          break;
        }
        case 'OPEN': {
          option.emitCallback &&
            option.emitCallback('event', 'open', {
              lunaId: lunaId.value,
              origin: origin.value,
              noFileTab: message?.noFileTab
            });
          break;
        }
        case 'FILE': {
          option.emitCallback &&
            option.emitCallback('event', 'file', {
              token: message?.SFTP_Token
            });
          break;
        }
        case 'CREATE_FILE_CONNECT_TOKEN': {
          option.emitCallback &&
            option.emitCallback('event', 'create-file-connect-token', {
              token: message?.SFTP_Token
            });
          break;
        }
      }
    });

    window.addEventListener(
      'resize',
      () => {
        fitAddon.fit();
      },
      false
    );

    window.SendTerminalData = data => {
      sendDataFromWindow(data);
    };

    window.Reconnect = () => {
      option.emitCallback && option.emitCallback('event', 'reconnect', '');
    };
  };

  /**
   * 初始化 CustomTerminal 相关事件
   */
  const initTerminalEvent = () => {
    if (terminal) {
      terminal.loadAddon(fitAddon);
      terminal.loadAddon(searchAddon);
      // terminal.loadAddon(new WebglAddon());

      terminal.open(el);
      terminal.focus();
      fitAddon.fit();

      terminal.onSelectionChange(() => {
        handleTerminalSelection(terminal!, termSelectionText);
      });
      terminal.attachCustomKeyEventHandler((e: KeyboardEvent) => {
        return handleCustomKey(e, terminal!, lunaId.value, origin.value);
      });
      terminal.onData((data: string) => {
        lastSendTime.value = new Date();
        handleTerminalOnData(data, type, terminalId.value, lunaConfig, socket);
      });
      terminal.onResize(({ cols, rows }) => {
        fitAddon.fit();
        handleTerminalResize(cols, rows, type, terminalId.value, socket);
      });
    }
  };

  /**
   * 创建非 k8s socket 连接
   */
  const createSocket = async (): Promise<WebSocket | undefined> => {
    let socketInstance: WebSocket;
    const url: string = generateWsURL();

    const { ws } = useWebSocket(url, {
      protocols: ['JMS-KOKO'],
      autoReconnect: {
        retries: 5,
        delay: 3000
      }
    });

    if (ws.value) {
      socketInstance = ws.value;

      return socketInstance;
    } else {
      message.error('Failed to create WebSocket connection');
    }
  };

  const createTerminal = async (config: ILunaConfig): Promise<Terminal> => {
    let terminalInstance: Terminal;

    const { fontSize, lineHeight, fontFamily } = config;

    const options = {
      allowProposedApi: true,
      fontSize,
      lineHeight,
      fontFamily,
      rightClickSelectsWord: true,
      theme: {
        background: '#1E1E1E'
      },
      scrollback: 5000
    };

    terminalInstance = new Terminal(options);

    return terminalInstance;
  };

  const initializeTerminal = (
    terminal: Terminal,
    socket: WebSocket,
    type: string
  ) => {
    initElEvent();
    initTerminalEvent();
    initCustomWindowEvent();

    const { createSentry } = useSentry(lastSendTime, option.i18nCallBack);
    sentry = createSentry(socket, terminal);

    initSocketEvent();
  };

  /**
   * 初始化事件总线相关事件
   */
  const initMittBusEvents = () => {
    mittBus.on('remove-event', () => {
      // @ts-ignore
      option.transSocket.removeEventListener('message', handleSocketMessage);
    });

    mittBus.on('terminal-search', ({ keyword, type = '' }) => {
      searchKeyWord(keyword, type);
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

    mittBus.on('remove-share-user', ({ sessionId, userMeta, type }) => {
      sendWsMessage(type, {
        session: sessionId,
        user_meta: userMeta
      });
    });

    mittBus.on('share-user', ({ type, query }) => {
      sendWsMessage(type, { query });
    });

    mittBus.on('sync-theme', ({ type, data }) => {
      sendWsMessage(type, data);
    });
  };

  onUnmounted(() => {
    mittBus.off('sync-theme');
    mittBus.off('share-user');
    mittBus.off('terminal-search');
    mittBus.off('create-share-url');
    mittBus.off('remove-share-user');
  });

  const init = async () => {
    const terminalStore = useTerminalStore();

    lunaConfig = terminalStore.getConfig;

    const [socketResult, terminalResult] = await Promise.allSettled([
      createSocket(),
      createTerminal(lunaConfig)
    ]);

    if (
      socketResult.status === 'fulfilled' &&
      terminalResult.status === 'fulfilled'
    ) {
      socket = socketResult.value!;
      terminal = terminalResult.value;

      initializeTerminal(terminal, socket, option.type);
      initMittBusEvents();
    } else {
      if (socketResult.status === 'rejected') {
        message.error('Socket error:', socketResult.reason);
      }
      if (terminalResult.status === 'rejected') {
        message.error('Terminal error:', terminalResult.reason);
      }
    }

    return terminal;
  };

  await init();

  return {
    terminal,
    setTerminalTheme
  };
};
