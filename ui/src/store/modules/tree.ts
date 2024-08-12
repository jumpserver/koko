import { defineStore } from 'pinia';
import type { ITreeState } from '@/store/interface';
import type { TreeOption } from 'naive-ui';
import type { customTreeOption } from '@/hooks/interface';
import { useTree } from '@/hooks/useTree.ts';

export const useTreeStore = defineStore('tree', {
    state: (): ITreeState => ({
        connectInfo: null,
        treeNodes: [],
        loadingTreeNode: false,
        currentNode: {}
    }),
    actions: {
        setConnectInfo(id: string, info: any, initTree: (key: string, label: string) => TreeOption[]) {
            this.connectInfo = info;

            this.treeNodes = initTree(id, info.asset.name);
        },
        loadTreeNode(
            socket: WebSocket,
            socketSend: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean,
            treeNodes: customTreeOption,
            id: string
        ) {
            const { syncLoadNode } = useTree(socket, socketSend);

            this.currentNode = treeNodes;
            this.loadingTreeNode = true;
            syncLoadNode(treeNodes, id);
        },
        setLoading(loading: boolean) {
            this.loadingTreeNode = loading;
        }
    }
});
