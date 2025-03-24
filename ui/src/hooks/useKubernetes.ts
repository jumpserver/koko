import { useKubernetesStore } from '@/store/modules/kubernetes.ts';
import { formatMessage, updateIcon } from '@/components/TerminalComponent/helper';
import { useTerminalStore } from '@/store/modules/terminal.ts';
import { base64ToUint8Array, generateWsURL } from './helper';
import { useParamsStore } from '@/store/modules/params.ts';
import { useTreeStore } from '@/store/modules/tree.ts';
import { useSentry } from '@/hooks/useZsentry.ts';
import { readText } from 'clipboard-polyfill';
import { useWebSocket } from '@vueuse/core';
import { preprocessInput } from '@/utils';
import { storeToRefs } from 'pinia';
import { h, ref } from 'vue';

import { createDiscreteApi, darkTheme, NIcon } from 'naive-ui';
import { Cube24Regular } from '@vicons/fluent';
import { Docker, Folder } from '@vicons/fa';
import { v4 as uuid } from 'uuid';

import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { SearchAddon } from '@xterm/addon-search';

import type { customTreeOption, ILunaConfig } from '@/hooks/interface';
import type { ISearchOptions } from '@xterm/addon-search';
import type { Ref } from 'vue';

import xtermTheme from 'xterm-theme';
import mittBus from '@/utils/mittBus.ts';
import { MaxTimeout } from '@/config';

const { message, notification } = createDiscreteApi(['message', 'notification'], {
  configProviderProps: {
    theme: darkTheme
  }
});

const handleConnected = (socket: WebSocket, pingInterval: Ref<number | null>) => {
  const kubernetesStore = useKubernetesStore();

  if (pingInterval.value) clearInterval(pingInterval.value);

  pingInterval.value = setInterval(() => {
    if (socket.CLOSED === socket.readyState || socket.CLOSING === socket.readyState) {
      return clearInterval(pingInterval.value!);
    }

    let currentDate: Date = new Date();

    if (kubernetesStore.lastReceiveTime.getTime() - currentDate.getTime() > MaxTimeout) {
      message.info('More than 30s do not receive data');
    }

    let pingTimeout: number = currentDate.getTime() - kubernetesStore.lastSendTime.getTime();

    if (pingTimeout < 0) return;

    socket.send(formatMessage(kubernetesStore.globalTerminalId, 'PING', ''));
  }, 25 * 1000);
};

/**
 * @description 初始化同步节点树
 */
export const initTreeNodes = (ws: WebSocket, id: string, info: any) => {
  const unique = uuid();
  const treeStore = useTreeStore();
  const sendData: string = JSON.stringify({
    type: 'TERMINAL_K8S_TREE'
  });

  const rootNode: customTreeOption = {
    id,
    key: unique,
    k8s_id: unique,
    isLeaf: false,
    isParent: true,
    socket: ws,
    label: info.asset.name,
    prefix: () =>
      h(NIcon, null, {
        default: () => h(Folder)
      })
  };

  treeStore.setRoot(rootNode);

  ws.send(sendData);
};

/**
 * 处理 socket Error
 *
 * @param {string} type
 */
export const handleInterrupt = (type: string) => {
  switch (type) {
    case 'error': {
      // terminal.write('Connection Websocket Error');
      message.error('Connection Websocket Error');
      break;
    }
    case 'disconnected': {
      // terminal.write('Connection Websocket Closed');
      message.info('Connection Websocket Closed');
      break;
    }
  }
};

/**
 * @description 设置通用属性
 *
 * @param nodes
 * @param label
 * @param isLeaf
 */
export const setCommonAttributes = (nodes: any, label: string, isLeaf: boolean) => {
  const unique = uuid();

  Object.assign(nodes, {
    label,
    key: unique,
    k8s_id: unique,
    isLeaf
  });
};

/**
 * 处理最后的 container 节点
 *
 * @param containers
 * @param podName
 * @param namespace
 * @param socket
 */
export const handleContainer = (containers: any, podName: string, namespace: string, socket: WebSocket) => {
  const kubernetesStore = useKubernetesStore();

  containers.forEach((container: any) => {
    Object.assign(container, {
      socket,
      namespace,
      key: uuid(),
      pod: podName,
      container: container.name,
      id: kubernetesStore.globalTerminalId,
      prefix: () => h(NIcon, { size: 16 }, { default: () => h(Docker) })
    });

    setCommonAttributes(container, container.name, true);
  });
};

/**
 * 处理 Pod
 *
 * @param pods
 * @param namespace
 * @param socket
 */
export const handlePods = (pods: any, namespace: string, socket: WebSocket) => {
  pods.forEach((pod: any) => {
    if (pod.containers && pod.containers?.length > 0) {
      pod.key = uuid();
      pod.label = pod.name;
      pod.isLeaf = false;
      pod.namespace = namespace;
      pod.children = pod.containers;
      pod.prefix = () => h(NIcon, { size: 16 }, { default: () => h(Cube24Regular) });

      // 处理最后的 container
      handleContainer(pod.children, pod.name, namespace, socket);

      delete pod.containers;
    } else {
      pod.children = [];
    }
  });
};

/**
 * 二次处理节点
 *
 * @param message
 * @param ws
 */
export const handleTreeNodes = (message: any, ws: WebSocket) => {
  const treeStore = useTreeStore();

  if (message.err) {
    treeStore.setTreeNodes({} as customTreeOption);

    return notification.error({
      content: message.err,
      duration: 5000
    });
  }

  const originNode = JSON.parse(message.data);

  if (Object.keys(originNode).length === 0) {
    return treeStore.setLoaded(false);
  }

  Object.keys(originNode).map(node => {
    // 得到每个 namespace
    const item = originNode[node];

    item.label = node;
    item.socket = ws;
    item.key = uuid();
    item.prefix = () => h(NIcon, { size: 15 }, { default: () => h(Folder) });

    if (item.pods && item.pods.length > 0) {
      // 处理 pods
      item.children = item.pods;

      handlePods(item.pods, item.name, ws);

      // 删除多余项
      delete item.pods;
    } else {
      delete item.pods;
      item.children = [];
    }

    treeStore.setTreeNodes(item);
  });

  treeStore.setLoaded(true);
};

/**
 * @description 处理 Tree 相关的 Socket 消息
 *
 * @param ws
 * @param event
 */
export const handleTreeMessage = (ws: WebSocket, event: MessageEvent) => {
  let type: string;
  let message: any;

  const treeStore = useTreeStore();
  const kubernetesStore = useKubernetesStore();
  const paramsStore = useParamsStore();

  kubernetesStore.setLastReceiveTime(new Date());

  if (!event.data) return;

  message = JSON.parse(event.data);
  type = message.type;

  switch (type) {
    case 'CLOSE':
    case 'ERROR': {
      ws.close();
      break;
    }
    case 'CONNECT': {
      const info = JSON.parse(message.data);

      //* 设置通用配置以及全局唯一 id
      paramsStore.setSetting(info.setting);
      kubernetesStore.setGlobalSetting(info.setting);
      kubernetesStore.setGlobalTerminalId(message.id);

      treeStore.setConnectInfo(info);

      updateIcon(info.setting);
      initTreeNodes(ws, message.id, info);

      break;
    }
    case 'TERMINAL_K8S_TREE': {
      handleTreeNodes(message, ws);
      break;
    }
  }
};

export const handleTerminalMessage = (ws: WebSocket, event: MessageEvent, createSentry: any, t: any) => {
  const treeStore = useTreeStore();
  const paramsStore = useParamsStore();
  const terminalStore = useTerminalStore();

  const { setting } = storeToRefs(paramsStore);

  const info = JSON.parse(event.data);

  // 根据返回信息的 k8s id 找到与之对应的 terminal 实例
  const operatedNode = treeStore.getTerminalByK8sId(info.k8s_id);
  const currentTerminal = operatedNode?.terminal;

  if (currentTerminal) {
    const sentry = createSentry(ws, currentTerminal);

    switch (info.type) {
      case 'TERMINAL_K8S_BINARY': {
        sentry.consume(base64ToUint8Array(info.raw));
        break;
      }
      case 'TERMINAL_SESSION': {
        const sessionInfo = JSON.parse(info.data);
        const sessionDetail = sessionInfo.session;

        const share = sessionInfo.permission.actions.includes('share');
        const username = `${sessionDetail.user}`;

        if (setting.value.SECURITY_WATERMARK_ENABLED) {
          operatedNode.waterMarkContent = `${username}\n${sessionDetail.asset.split('(')[0]}`;
        }

        if (sessionInfo.backspaceAsCtrlH) {
          const value = sessionInfo.backspaceAsCtrlH ? '1' : '0';

          terminalStore.setTerminalConfig('backspaceAsCtrlH', value);
        }

        if (setting.value.SECURITY_SESSION_SHARE && share) {
          operatedNode.enableShare = true;
        }

        if (operatedNode.sessionIdMap) {
          operatedNode.sessionIdMap.set(info.k8s_id, sessionDetail.id);
        } else {
          operatedNode.sessionIdMap = new Map();
          operatedNode.sessionIdMap.set(info.k8s_id, sessionDetail.id);
        }

        if (operatedNode.ctrlCAsCtrlZMap) {
          operatedNode.ctrlCAsCtrlZMap.set(info.k8s_id, sessionInfo.ctrlCAsCtrlZ ? '1' : '0');
        } else {
          operatedNode.ctrlCAsCtrlZMap = new Map();
          operatedNode.ctrlCAsCtrlZMap.set(info.k8s_id, sessionInfo.ctrlCAsCtrlZ ? '1' : '0');
        }

        operatedNode.themeName = sessionInfo.themeName;

        treeStore.setK8sIdMap(info.k8s_id, { ...operatedNode });

        currentTerminal.options.theme = xtermTheme[sessionInfo.themeName];

        break;
      }
      case 'TERMINAL_ACTION': {
        break;
      }
      case 'TERMINAL_ERROR': {
        const hasCurrentK8sId = treeStore.removeK8sIdMap(info.k8s_id);

        if (hasCurrentK8sId) {
          currentTerminal?.write(info.err);
        }

        break;
      }
      case 'K8S_CLOSE': {
        treeStore.removeK8sIdMap(info.k8s_id);

        currentTerminal?.attachCustomKeyEventHandler(() => {
          return false;
        });

        operatedNode.enableShare = false;

        if (operatedNode.onlineUsersMap && operatedNode.onlineUsersMap.hasOwnProperty(info.id)) {
          delete operatedNode.onlineUsersMap[info.id];
        }

        treeStore.setK8sIdMap(info.k8s_id, { ...operatedNode });

        break;
      }
      case 'TERMINAL_SHARE_JOIN': {
        const data = JSON.parse(info.data);
        const k8s_id: string = info.k8s_id;

        if (operatedNode.onlineUsersMap && operatedNode.onlineUsersMap[k8s_id]) {
          operatedNode.onlineUsersMap[k8s_id].push({ k8s_id: info.k8s_id, ...data });
          treeStore.setK8sIdMap(k8s_id, { ...operatedNode });
        } else {
          operatedNode.onlineUsersMap = {};
          operatedNode.onlineUsersMap[k8s_id] = [{ k8s_id, ...data }];

          treeStore.setK8sIdMap(k8s_id, { ...operatedNode });
        }

        if (data.primary) {
          break;
        }

        message.info(`${data.user} ${t('JoinShare')}`);

        break;
      }
      case 'TERMINAL_SHARE_LEAVE': {
        const data = JSON.parse(info.data);
        const k8s_id: string = info.k8s_id;

        if (operatedNode.onlineUsersMap.hasOwnProperty(k8s_id)) {
          const items = operatedNode?.onlineUsersMap[k8s_id];
          const index = items.findIndex((item: any) => item?.terminal_id === data?.terminal_id);

          if (index !== -1) {
            items.splice(index, 1);

            if (items.length === 0) {
              delete operatedNode.onlineUsersMap[k8s_id];
            }
          }
        }

        treeStore.setK8sIdMap(k8s_id, { ...operatedNode });

        message.info(`${data.user} ${t('LeaveShare')}`);

        break;
      }
      case 'CLOSE': {
        operatedNode.enableShare = false;

        treeStore.setK8sIdMap(info.k8s_id, { ...operatedNode });
        break;
      }
    }
  }

  // 由于 TERMINAL_GET_SHARE_USER 不会返回 k8s id 所以只能根据当前页保存的 k8s id 去获取 node 信息
  if (info.type === 'TERMINAL_GET_SHARE_USER') {
    const innerOperatedNode = treeStore.getTerminalByK8sId(terminalStore.currentTab);
    innerOperatedNode.userOptions = JSON.parse(info.data);

    treeStore.setK8sIdMap(terminalStore.currentTab, { ...innerOperatedNode });
  }

  if (info.type === 'TERMINAL_SHARE') {
    const data = JSON.parse(info.data);

    paramsStore.setShareId(data.share_id);
    paramsStore.setShareCode(data.code);
  }
};

/**
 * @description 创建 k8s 连接
 */
export const createConnect = (t: any) => {
  let pingInterval: Ref<number | null> = ref(null);
  let connectURL: string = generateWsURL();

  const { createSentry } = useSentry();

  if (connectURL) {
    const { ws } = useWebSocket(connectURL, {
      protocols: ['JMS-KOKO'],
      onConnected: (ws: WebSocket) => handleConnected(ws, pingInterval),
      onMessage: (ws: WebSocket, event: MessageEvent) => {
        handleTreeMessage(ws, event);
        handleTerminalMessage(ws, event, createSentry, t);
      },
      onError: () => handleInterrupt('error'),
      onDisconnected: () => handleInterrupt('disconnected')
    });

    return ws.value;
  }
};

/**
 * @description 初始化终端事件
 *
 * @param el
 * @param terminal
 * @param lunaConfig
 * @param socket
 */
export const initTerminalEvent = (
  el: HTMLElement,
  terminal: Terminal,
  lunaConfig: ILunaConfig,
  socket: WebSocket
) => {
  let fitAddon: FitAddon = new FitAddon();
  let searchAddon: SearchAddon = new SearchAddon();

  const terminalStore = useTerminalStore();

  terminal.loadAddon(fitAddon);
  terminal.loadAddon(searchAddon);

  terminal.open(el);
  terminal.focus();
  fitAddon.fit();

  terminal.onResize(({ cols, rows }) => {
    const treeStore = useTreeStore();
    const terminalStore = useTerminalStore();

    const currentNode = treeStore.getTerminalByK8sId(terminalStore.currentTab);

    fitAddon.fit();

    const resizeData = JSON.stringify({ cols, rows });
    const sendData = {
      id: currentNode.id,
      k8s_id: currentNode.k8s_id,
      type: 'TERMINAL_K8S_RESIZE',
      namespace: currentNode.namespace || '',
      pod: currentNode.pod || '',
      container: currentNode.container || '',
      resizeData
    };

    socket.send(JSON.stringify(sendData));
  });

  terminal.onData((data: string) => {
    const treeStore = useTreeStore();
    const kubernetesStore = useKubernetesStore();

    kubernetesStore.setLastSendTime(new Date());

    const inputMessage = preprocessInput(data, lunaConfig);
    const currentTabInfo = treeStore.currentNode;

    const messageBody = {
      data: inputMessage,
      id: currentTabInfo?.id,
      pod: currentTabInfo?.pod || '',
      k8s_id: currentTabInfo?.k8s_id,
      namespace: currentTabInfo.namespace || '',
      container: currentTabInfo.container || '',
      type: 'TERMINAL_K8S_DATA'
    };

    socket.send(JSON.stringify(messageBody));
  });

  terminal.onSelectionChange(() => {
    terminalStore.setTerminalConfig('termSelectionText', terminal.getSelection().trim());
  });

  terminal.attachCustomKeyEventHandler(e => {
    if (e.altKey && e.shiftKey && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
      switch (e.key) {
        case 'ArrowRight':
          mittBus.emit('alt-shift-right');

          break;
        case 'ArrowLeft':
          mittBus.emit('alt-shift-left');
          break;
      }
      return false;
    }

    if (e.ctrlKey && e.key === 'c' && terminal.hasSelection()) {
      return false;
    }

    return !(e.ctrlKey && e.key === 'v');
  });

  return {
    fitAddon,
    searchAddon
  };
};

/**
 * @description 初始节点相关事件
 */
export const initElEvent = (
  el: HTMLElement,
  terminal: Terminal,
  fitAddon: FitAddon,
  socket: WebSocket,
  lunaConfig: ILunaConfig
) => {
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
    async e => {
      if (e.ctrlKey || lunaConfig.quickPaste !== '1') return;

      let text: string = '';

      const terminalStore = useTerminalStore();
      const kubernetesStore = useKubernetesStore();

      try {
        text = await readText();
      } catch (e) {
        if (terminalStore.termSelectionText !== '') text = terminalStore.termSelectionText;
      } finally {
        socket.send(
          JSON.stringify({
            id: kubernetesStore.globalTerminalId,
            k8s_id: terminalStore.currentTab,
            type: 'TERMINAL_K8S_DATA',
            data: text
          })
        );
      }

      e.preventDefault();
    },
    false
  );
};

/**
 * @description 初始化全局 window 事件
 *
 * @param fitAddon
 */
export const initCustomWindowEvent = (fitAddon: FitAddon) => {
  window.addEventListener(
    'resize',
    () => {
      fitAddon.fit();
    },
    false
  );

  window.addEventListener('keydown', (event: KeyboardEvent) => {
    const isAltShift = event.altKey && event.shiftKey;

    if (isAltShift && event.key === 'ArrowLeft') {
      mittBus.emit('alt-shift-left');
    } else if (isAltShift && event.key === 'ArrowRight') {
      mittBus.emit('alt-shift-right');
    }
  });
};

/**
 * @description 发送 k8s 事件
 *
 * @param socket
 * @param type
 * @param data
 */
export const sendK8sMessage = (socket: WebSocket, type: string, data: any) => {
  const treeStore = useTreeStore();
  const terminalStore = useTerminalStore();

  const currentNode = treeStore.getTerminalByK8sId(terminalStore.currentTab);

  socket.send(
    JSON.stringify({
      id: currentNode.id,
      k8s_id: currentNode.k8s_id,
      type,
      data: JSON.stringify(data)
    })
  );
};

export const initMittBusEvents = (searchAddon: SearchAddon, socket: WebSocket) => {
  mittBus.on('terminal-search', ({ keyword, type = '' }) => {
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
  });
  mittBus.on('create-share-url', ({ type, sessionId, shareLinkRequest }) => {
    const origin = window.location.origin;

    sendK8sMessage(socket, type, {
      origin,
      session: sessionId,
      users: shareLinkRequest.users,
      expired_time: shareLinkRequest.expiredTime,
      action_permission: shareLinkRequest.actionPerm
    });
  });
  mittBus.on('remove-share-user', ({ sessionId, userMeta, type }) => {
    sendK8sMessage(socket, type, {
      session: sessionId,
      user_meta: userMeta
    });
  });
  mittBus.on('share-user', ({ type, query }) => {
    sendK8sMessage(socket, type, { query });
  });
  mittBus.on('sync-theme', ({ type, data }) => {
    sendK8sMessage(socket, type, data);
  });
};

/**
 * @description 创建 K8s 终端
 */
export const createTerminal = (el: HTMLElement, socket: WebSocket, lunaConfig: ILunaConfig) => {
  const { fontSize, lineHeight, fontFamily } = lunaConfig;

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

  const terminal: Terminal = new Terminal(options);

  const { fitAddon, searchAddon } = initTerminalEvent(el, terminal, lunaConfig, socket);

  initElEvent(el, terminal, fitAddon, socket, lunaConfig);
  initCustomWindowEvent(fitAddon);
  initMittBusEvents(searchAddon, socket);

  return terminal;
};

export const useKubernetes = (t: any) => {
  let socket: WebSocket | undefined;

  const ws = createConnect(t);

  if (ws) {
    socket = ws;
    socket!.binaryType = 'arraybuffer';

    return socket;
  }
};
