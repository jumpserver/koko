import { ref, h } from 'vue';
import { storeToRefs } from 'pinia';
import { v4 as uuidv4 } from 'uuid';

// 组件
import { NIcon } from 'naive-ui';
import { Folder } from '@vicons/ionicons5';
import SvgIcon from '@/components/SvgIcon/index.vue';

// 引入 Hook
import { useLogger } from './useLogger.ts';
import { useWebSocket } from '@vueuse/core';
import { createDiscreteApi } from 'naive-ui';

// 引入 Store
import { useTreeStore } from '@/store/modules/tree.ts';

// 类型
import type { Ref } from 'vue';
import type { customTreeOption } from '@/hooks/interface';

import { generateWsURL, onWebsocketOpen, onWebsocketWrong } from './helper';
import { updateIcon, wsIsActivated } from '@/components/CustomTerminal/helper/index.ts';
import { useParamsStore } from '@/store/modules/params.ts';

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
     * @param treeNode
     */
    const syncLoadNodes = (treeNode: customTreeOption) => {
        if (!treeNode) return;

        if (!beforeLoad(treeNode)) return;

        debug('Start Load Tree Node ....');

        const loadDate: customTreeOption = {
            id: terminalId.value,
            pod: treeNode.pod || '',
            type: 'TERMINAL_K8S_TREE',
            k8s_id: treeNode.id,
            namespace: treeNode.namespace || ''
        };

        if (wsIsActivated(socket)) {
            socket?.send(JSON.stringify(loadDate));
        }
    };

    /**
     * @description 初始化节点
     * @param key
     * @param label
     */
    const initTree = (key: string, label: string) => {
        treeStore.setTreeNodes({
            key,
            label,
            id: key,
            k8s_id: uuidv4(),
            isLeaf: false,
            isParent: true,
            prefix: () =>
                h(NIcon, null, {
                    default: () => h(Folder)
                })
        });
    };

    /**
     * @description 处理子节点
     * @param name
     * @param nodeId
     */
    const handleChildNodes = (name: string, nodeId: string) => {
        const node = currentNode.value;

        if (!node) {
            debug('currentNode is undefined or null');
            return null;
        }

        const childNode: customTreeOption = {
            id: nodeId,
            key: uuidv4(),
            k8s_id: uuidv4(),
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
            childNode.namespace = node.namespace;
            childNode.pod = node.pod;
            childNode.container = name;
            childNode.isLeaf = true;
            childNode.prefix = () =>
                h(SvgIcon, {
                    name: 'k8s',
                    iconStyle: {
                        width: '14px',
                        height: '14px',
                        fill: '#D8D8D8'
                    }
                });
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

            if (!currentNode.value) {
                debug('currentNode is undefined or null');
                return [];
            }

            const childNodes = data.map((name: string) => {
                return handleChildNodes(name, nodeId);
            });

            if (!currentNode.value.children && !currentNode.value.isLeaf) {
                currentNode.value.children = childNodes;
            }

            return childNodes;
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
                terminalId.value = msg.id;

                const info = JSON.parse(msg.data);

                paramsStore.setSetting(info.setting);

                updateIcon(info.setting);

                message.info('K8s Websocket Connection Established');

                // 初始化 Tree
                treeStore.setConnectInfo(info);
                initTree(msg.id, info.asset.name);

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
                message.error('Receive Connection Closed');

                socket.close();
                break;
            }
            default: {
                // todo 设置 msg.k8s_id, msg
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
