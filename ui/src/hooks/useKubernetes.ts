import { useKubernetesStore } from '@/store/modules/kubernetes.ts';
import { updateIcon } from '@/components/CustomTerminal/helper';
import { useTreeStore } from '@/store/modules/tree.ts';
import { useWebSocket } from '@vueuse/core';
import { generateWsURL } from './helper';

import type { Terminal } from '@xterm/xterm';
import type { customTreeOption } from '@/hooks/interface';
import { h } from 'vue';
import { createDiscreteApi, darkTheme, NIcon } from 'naive-ui';
import { v4 as uuid } from 'uuid';
import { Docker, Folder } from '@vicons/fa';
import { Cube24Regular } from '@vicons/fluent';

const { message, notification } = createDiscreteApi(['message', 'notification'], {
    configProviderProps: {
        theme: darkTheme
    }
});

//todo)) Error 和 Disconnected 需要等到 Terminal 创建后再去添加
//todo)) 心跳机制

const handleConnected = () => {};

/**
 * @description 初始化同步节点树
 */
export const initTreeNodes = (ws, id: string, info: any) => {
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
 * @param {Terminal} terminal
 */
export const handleInterrupt = (terminal: Terminal, type: string) => {
    switch (type) {
        case 'error': {
            terminal.write('Connection Websocket Error');
            break;
        }
        case 'disconnected': {
            terminal.write('Connection Websocket Closed');
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
export const setCommonAttributes = (nodes, label, isLeaf) => {
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
 */
export const handleContainer = (containers, podName, namespace) => {
    const kubernetesStore = useKubernetesStore();

    containers.forEach(container => {
        Object.assign(container, {
            namespace,
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
 */
export const handlePods = (pods: any, namespace: string) => {
    pods.forEach(pod => {
        if (pod.containers && pod.containers?.length > 0) {
            pod.label = pod.name;
            pod.isLeaf = false;
            pod.namespace = namespace;
            pod.children = pod.containers;
            pod.prefix = () => h(NIcon, { size: 16 }, { default: () => h(Cube24Regular) });

            // 处理最后的 container
            handleContainer(pod.children, pod.name, namespace);

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
 */
export const handleTreeNodes = (message: any) => {
    const treeStore = useTreeStore();

    if (message.err) {
        treeStore.setTreeNodes({} as customTreeOption);

        return notification.error({
            content: msg.err,
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
        item.prefix = () => h(NIcon, { size: 15 }, { default: () => h(Folder) });

        if (item.pods && item.pods.length > 0) {
            // 处理 pods
            item.children = item.pods;

            handlePods(item.pods, item.name);

            // 删除多余项
            delete item.pods;
        } else {
            item.children = [];
        }

        treeStore.setTreeNodes(node);
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
            kubernetesStore.setGlobalSetting(info.setting);
            kubernetesStore.setGlobalTerminalId(message.id);

            treeStore.setConnectInfo(info);

            updateIcon(info.setting);
            initTreeNodes(ws, message.id, info);

            break;
        }
        case 'TERMINAL_K8S_TREE': {
            handleTreeNodes(message);
            break;
        }
    }
};

/**
 * @description 创建 k8s 连接
 */
export const createConnect = () => {
    let connectURL: string = generateWsURL();

    if (connectURL) {
        const { ws } = useWebSocket(connectURL, {
            protocols: ['JMS-KOKO'],
            onConnected: () => handleConnected,
            onMessage: (ws: WebSocket, event: MessageEvent) => handleTreeMessage(ws, event)
        });

        return ws.value;
    }
};

export const useKubernetes = () => {
    let socket: WebSocket | undefined;

    const ws = createConnect();

    if (ws) {
        socket = ws;
        socket!.binaryType = 'arraybuffer';

        return socket;
    }
};
