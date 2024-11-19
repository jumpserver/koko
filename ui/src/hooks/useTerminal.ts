// Terminal 相关
import xtermTheme from 'xterm-theme';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
// import { WebglAddon } from '@xterm/addon-webgl';
import { ISearchOptions, SearchAddon } from '@xterm/addon-search';
import { Sentry } from 'nora-zmodemjs/src/zmodem_browser';
import { defaultTheme } from '@/config';

// hook
import { createDiscreteApi } from 'naive-ui';
import { useSentry } from '@/hooks/useZsentry.ts';
import { useWebSocket } from '@vueuse/core';

// store
import { storeToRefs } from 'pinia';
import { useTerminalStore } from '@/store/modules/terminal.ts';
import { useParamsStore } from '@/store/modules/params.ts';

import { onUnmounted, ref, Ref } from 'vue';
import { writeBufferToTerminal } from '@/utils';
import type { ILunaConfig } from '@/hooks/interface';

// 工具函数
import {
  base64ToUint8Array,
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
} from '@/components/CustomTerminal/helper';
import mittBus from '@/utils/mittBus.ts';
import { useTreeStore } from '@/store/modules/tree.ts';

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
  emitCallback?: (e: string, type: string, msg: any, terminal?: Terminal) => void;

  // t
  i18nCallBack?: (key: string) => string;
}

const { message } = createDiscreteApi(['message']);

export const useTerminal = async (el: HTMLElement, option: ICallbackOptions): Promise<ITerminalInstance> => {
  let sentry: Sentry;
  let socket: WebSocket;
  let terminal: Terminal | undefined;
  let lunaConfig: ILunaConfig;

  let fitAddon: FitAddon = new FitAddon();
  let searchAddon: SearchAddon = new SearchAddon();

  let type: string = option.type;

  let lunaId: Ref<string> = ref('');
  let origin: Ref<string> = ref('');
  let k8s_id: Ref<string> = ref('');
  let terminalId: Ref<string> = ref('');
  let termSelectionText: Ref<string> = ref('');
  let pingInterval: Ref<number | null> = ref(null);

  let lastSendTime: Ref<Date> = ref(new Date());
  let lastReceiveTime: Ref<Date> = ref(new Date());

  let messageHandlers = {};
  let handleSocketMessage: any;

  const dispatch = (data: string) => {
    if (!data) return;

    let msg = JSON.parse(data);

    const terminalStore = useTerminalStore();
    const paramsStore = useParamsStore();

    const { enableZmodem, zmodemStatus } = storeToRefs(terminalStore);

    switch (msg.type) {
      case 'CONNECT': {
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

        socket.send(formatMessage(terminalId.value, 'TERMINAL_INIT', JSON.stringify(terminalData)));
        break;
      }
      case 'CLOSE': {
        socket.close();
        sendEventToLuna('CLOSE', '');
        break;
      }
      case 'PING':
        break;
      case 'TERMINAL_ACTION': {
        const action = msg.data;

        switch (action) {
          case 'ZMODEM_START': {
            terminalStore.setTerminalConfig('zmodemStatus', true);

            // if (enableZmodem.value) {
            //     option.i18nCallBack && message.info(option.i18nCallBack('WaitFileTransfer'));
            // }
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
      case 'TERMINAL_ERROR':
      case 'ERROR': {
        terminal?.write(msg.err);
        break;
      }
      case 'MESSAGE_NOTIFY': {
        break;
      }
      case 'TERMINAL_SHARE_USER_REMOVE': {
        option.i18nCallBack && message.info(option.i18nCallBack('RemoveShareUser'));
        socket.close();
        break;
      }
      default: {
      }
    }

    option.emitCallback && option.emitCallback('socketData', msg.type, msg, terminal);
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
  const setTerminalTheme = (themeName: string, terminal: Terminal, emits: any) => {
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
    if (option.type === 'k8s') {
      const treeStore = useTreeStore();
      const { currentNode } = storeToRefs(treeStore);

      return socket?.send(
        JSON.stringify({
          k8s_id: currentNode.value.k8s_id,
          type,
          data: JSON.stringify(data)
        })
      );
    }

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
          console.log(e);
        }
      } else {
        writeBufferToTerminal(enableZmodem.value, zmodemStatus.value, terminal!, event.data);
      }
    } else {
      dispatch(event.data);
    }
  };

  /**
   * 处理 K8s 的 message 事件
   *
   * @param socketData
   */
  const handleK8sMessage = (socketData: any) => {
    const treeStore = useTreeStore();

    switch (socketData.type) {
      case 'TERMINAL_K8S_BINARY': {
        terminalId.value = socketData.id;
        k8s_id.value = socketData.k8s_id;

        const term = treeStore.getTerminalByK8sId(socketData.k8s_id)?.terminal;

        if (term) {
          const { createSentry } = useSentry(lastSendTime, option.i18nCallBack);

          sentry = createSentry(socket, term);
          sentry.consume(base64ToUint8Array(socketData.raw));
        }

        break;
      }
      case 'TERMINAL_ACTION': {
        const action = socketData.data;

        switch (action) {
          case 'ZMODEM_START': {
            option.i18nCallBack && message.warning(option.i18nCallBack('CustomTerminal.WaitFileTransfer'));
            break;
          }
          case 'ZMODEM_END': {
            option.i18nCallBack && message.warning(option.i18nCallBack('CustomTerminal.EndFileTransfer'));
            terminal?.writeln('\r\n');
            break;
          }
        }
        break;
      }
      case 'TERMINAL_ERROR': {
        const hasCurrentK8sId = treeStore.removeK8sIdMap(socketData.k8s_id);

        if (hasCurrentK8sId) {
          const term: Terminal = treeStore.getTerminalByK8sId(socketData.k8s_id)?.terminal;

          term?.write(socketData.err);
        }

        break;
      }
      case 'K8S_CLOSE': {
        const treeStore = useTreeStore();

        const id = socketData.k8s_id;

        if (id) {
          // @ts-ignore
          const handler = messageHandlers[id];

          if (handler) {
            socket.removeEventListener('message', handler);
            // @ts-ignore
            delete messageHandlers[id];
          }
        }

        const hasCurrentK8sId = treeStore.removeK8sIdMap(socketData.k8s_id);

        // 如果 hasCurrentK8sId 为 true 表明需要操作的是当前的 k8s_id 的 terminal
        if (hasCurrentK8sId) {
          const term: Terminal = treeStore.getTerminalByK8sId(socketData.k8s_id)?.terminal;

          term?.attachCustomKeyEventHandler(() => {
            return false;
          });
        }

        break;
      }
      default: {
        break;
      }
    }

    option.emitCallback && option.emitCallback('socketData', socketData.type, socketData, terminal);
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
   * 初始非 k8s 的 socket 事件
   */
  const initSocketEvent = () => {
    if (socket) {
      socket.onopen = () => {
        onWebsocketOpen(socket, lastSendTime.value, terminalId.value, pingInterval, lastReceiveTime);
      };
      socket.onmessage = (event: MessageEvent) => {
        if (type === 'common') {
          handleMessage(event);
        }
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
        handleContextMenu(e, lunaConfig, socket!, terminalId.value, termSelectionText.value, k8s_id.value);
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
        case 'CMD': {
          sendDataFromWindow(message.data);
          break;
        }
        case 'FOCUS': {
          terminal?.focus();
          break;
        }
        case 'OPEN': {
          option.emitCallback && option.emitCallback('event', 'open', '');
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

    if (option.type === 'k8s') {
      window.addEventListener('keydown', (event: KeyboardEvent) => {
        const isAltShift = event.altKey && event.shiftKey;

        if (isAltShift && event.key === 'ArrowLeft') {
          mittBus.emit('alt-shift-left');
        } else if (isAltShift && event.key === 'ArrowRight') {
          mittBus.emit('alt-shift-right');
        }
      });
    }

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
    if (type === 'k8s') {
      return Promise.resolve(option.transSocket);
    }

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

  const initializeTerminal = (terminal: Terminal, socket: WebSocket, type: string) => {
    initElEvent();
    initTerminalEvent();
    initCustomWindowEvent();

    const { createSentry } = useSentry(lastSendTime, option.i18nCallBack);
    sentry = createSentry(socket, terminal);

    if (type === 'k8s') {
      const treeStore = useTreeStore();
      const { currentNode } = storeToRefs(treeStore);

      const { currentTab } = storeToRefs(useTerminalStore());

      treeStore.setK8sIdMap(currentNode.value.k8s_id!, {
        terminal,
        socket,
        ...currentNode.value
      });

      messageHandlers = {
        [currentTab.value]: (e: MessageEvent) => {
          handleK8sMessage(JSON.parse(e.data));
        }
      };

      handleSocketMessage = (e: MessageEvent) => {
        // @ts-ignore
        const handler = messageHandlers[currentTab.value];

        if (handler) {
          handler(e);
        }
      };

      option.transSocket?.addEventListener('message', handleSocketMessage);
    } else {
      initSocketEvent();
    }
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
      if (option.type === 'k8s') {
        return socket.send(
          JSON.stringify({
            k8s_id: k8s_id.value,
            id: terminalId.value,
            type,
            data: JSON.stringify(data)
          })
        );
      }

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

    if (socketResult.status === 'fulfilled' && terminalResult.status === 'fulfilled') {
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
