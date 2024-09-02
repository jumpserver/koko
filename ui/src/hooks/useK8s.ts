import { NIcon } from 'naive-ui';
import { v4 as uuid } from 'uuid';
import { Folder } from '@vicons/fa';
import { Kubernetes } from '@vicons/carbon';

import { ref, h } from 'vue';
import { storeToRefs } from 'pinia';
import { useLogger } from './useLogger.ts';
import { useWebSocket } from '@vueuse/core';
import { createDiscreteApi } from 'naive-ui';
import { useTreeStore } from '@/store/modules/tree.ts';
import { useParamsStore } from '@/store/modules/params.ts';
import { generateWsURL, onWebsocketOpen, onWebsocketWrong } from './helper';
import { updateIcon, wsIsActivated } from '@/components/CustomTerminal/helper/index.ts';

import type { Ref } from 'vue';
import type { customTreeOption } from '@/hooks/interface';

const { debug } = useLogger('K8s');
const { message } = createDiscreteApi(['message']);

export const useK8s = () => {
    const treeStore = useTreeStore();

    const { currentNode } = storeToRefs(treeStore);

    let socket: WebSocket | undefined;

    let terminalId: Ref<string> = ref('');
    let lastSendTime: Ref<Date> = ref(new Date());
    let lastReceiveTime: Ref<Date> = ref(new Date());
    let pingInterval: Ref<number | null> = ref(null);

    /**
     * 节点加载前的校验
     */
    const beforeLoad = (treeNode: customTreeOption) => {
        return treeNode && !treeNode.children && !treeNode.isLeaf;
    };

    /**
     * @description 点击节点触发异步 Load 或者直接连接终端
     * @param node
     */
    const syncLoadNodes = (node: customTreeOption) => {
        if (!node) return;

        if (!beforeLoad(node)) return;

        debug('Start Load Tree Node ....');

        const currentNode: customTreeOption = {
            type: 'TERMINAL_K8S_TREE',
            k8s_id: node.k8s_id,
            pod: node.pod || '',
            namespace: node.namespace || ''
        };

        if (wsIsActivated(socket)) {
            socket?.send(JSON.stringify(currentNode));
        }
    };

    /**
     * @description 初始化节点
     * @param key
     * @param rootNodeName
     */
    const initTree = (key: string, rootNodeName: string) => {
        const treeRootNode: customTreeOption = {
            id: key,
            key,
            label: rootNodeName,
            k8s_id: uuid(),
            isLeaf: false,
            isParent: true,
            prefix: () =>
                h(NIcon, null, {
                    default: () => h(Folder)
                })
        };

        syncLoadNodes(treeRootNode);

        treeStore.setTreeNodes(treeRootNode);
        treeStore.setCurrentNode(treeRootNode);
    };

    /**
     * @description 处理子节点
     * @param name
     * @param nodeId
     */
    const handleChildNodes = (name: string, nodeId: string): customTreeOption => {
        const node = currentNode.value;

        if (!node) {
            debug('currentNode is undefined or null');
            return {};
        }

        const childNode: customTreeOption = {
            key: uuid(),
            k8s_id: uuid(),
            label: name,
            isLeaf: false,
            prefix: () =>
                h(NIcon, null, {
                    default: () => h(Folder)
                })
        };

        if (!node.namespace && !node.pod) {
            childNode.namespace = name;
        } else if (node.namespace && !node.pod) {
            childNode.namespace = node.namespace;
            childNode.pod = name;
        } else if (node.namespace && node.pod && !node.container) {
            childNode.isLeaf = true;
            childNode.id = nodeId;
            childNode.pod = node.pod;
            childNode.container = name;
            childNode.namespace = node.namespace;
            childNode.prefix = () =>
                h(
                    NIcon,
                    { size: 20 },
                    {
                        default: () => h(Kubernetes)
                    }
                );
        }

        return childNode;
    };

    /**
     * @description 更新节点
     * @param msg
     */
    const updateTreeNodes = (msg: any) => {
        try {
            const nodeId = msg.id;
            const data = JSON.parse(msg.data);

            const childNodes = data.map((name: string): customTreeOption => {
                return handleChildNodes(name, nodeId);
            });

            // 如果当前鼠标点击节点没有 children，则为 treeNode 增加子元素
            if (!currentNode.value.children && !currentNode.value.isLeaf) {
                return treeStore.setChildren(childNodes);
            }
        } catch (e) {
            message.error(`Error parsing message or updating tree nodes: ${e}`);
            return [];
        }
    };

    /**
     * 处理 Socket 消息
     */
    const handleMessage = (socket: WebSocket, event: MessageEvent) => {
        const treeStore = useTreeStore();
        const paramsStore = useParamsStore();

        lastSendTime.value = new Date();

        if (event.data === undefined) return;

        let msg = JSON.parse(event.data);

        switch (msg.type) {
            case 'CONNECT': {
                const info = JSON.parse(msg.data);
                const rootNodeName = info.asset.name;

                terminalId.value = msg.id;
                paramsStore.setSetting(info.setting);
                updateIcon(info.setting);

                treeStore.setConnectInfo(info);
                initTree(msg.id, rootNodeName);

                break;
            }
            case 'TERMINAL_K8S_TREE': {
                updateTreeNodes(msg);

                break;
            }
            case 'PING': {
                break;
            }
            case 'CLOSE':
            case 'ERROR': {
                socket.close();
                break;
            }
            default: {
                break;
            }
        }
    };

    /**
     * 创建 Tree 的 Socket 连接
     */
    const createTreeConnect = () => {
        const connectURL = generateWsURL();

        const { ws } = useWebSocket(connectURL, {
            protocols: ['JMS-KOKO'],
            onConnected: (socket: WebSocket) => {
                onWebsocketOpen(socket, lastSendTime.value, terminalId.value, pingInterval, lastReceiveTime);
            },
            onError: (_ws: WebSocket, event: Event) => {
                onWebsocketWrong(event, 'error');
            },
            onDisconnected: (_ws: WebSocket, event: CloseEvent) => {
                onWebsocketWrong(event, 'disconnected');
            },
            onMessage: (socket: WebSocket, event: MessageEvent) => {
                handleMessage(socket, event);
            }
        });

        socket = ws.value;

        return ws.value;
    };

    return {
        syncLoadNodes,
        createTreeConnect
    };
};
