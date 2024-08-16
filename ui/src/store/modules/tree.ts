import { defineStore } from 'pinia';

import type { TreeOption } from 'naive-ui';
import type { ITreeState } from '@/store/interface';
import type { customTreeOption } from '@/hooks/interface';

export const useTreeStore = defineStore('tree', {
    state: (): ITreeState => ({
        connectInfo: null,
        treeNodes: [],
        loadingTreeNode: false,
        currentNode: {}
    }),
    actions: {
        setTreeNodes(nodes: TreeOption) {
            this.treeNodes.push(nodes);
        },
        setConnectInfo(info: any) {
            this.connectInfo = info;
        },
        setCurrentNode(currentNode: customTreeOption) {
            this.currentNode = currentNode;
        },
        setLoading(loading: boolean) {
            this.loadingTreeNode = loading;
        }
    }
});
