import { defineStore } from 'pinia';

import type { TreeOption } from 'naive-ui';
import type { ITreeState } from '@/store/interface';
import type { customTreeOption } from '@/hooks/interface';

export const useTreeStore = defineStore('tree', {
    state: (): ITreeState => ({
        connectInfo: null,
        treeNodes: [],
        currentNode: {},
        root: {},
        isLoaded: false,
        terminalMap: new Map()
    }),
    actions: {
        setTreeNodes(nodes: customTreeOption) {
            this.treeNodes.push(nodes);
        },
        setChildren(nodes: customTreeOption[]) {
            const updateChildren = (tree: TreeOption[]) => {
                for (const node of tree) {
                    if (node.k8s_id === this.currentNode.k8s_id) {
                        node.children = nodes;
                        return true;
                    } else if (node.children && node.children.length > 0) {
                        const found = updateChildren(node.children);
                        if (found) return true;
                    }
                }
                return false;
            };

            if (this.treeNodes.length > 0) {
                updateChildren(this.treeNodes);
            }
        },
        setConnectInfo(info: any) {
            this.connectInfo = info;
        },
        setCurrentNode(currentNode: customTreeOption) {
            this.currentNode = currentNode;
        },
        setRoot(node: customTreeOption) {
            this.root = node;
        },
        setReload() {
            this.treeNodes = [];
        },
        setLoaded(status: boolean) {
            this.isLoaded = status;
        },
        setK8sIdMap(k8s_id: string, data: any) {
            this.terminalMap.set(k8s_id, data);
        },
        getTerminalByK8sId(k8s_id: string): any {
            return this.terminalMap.get(k8s_id);
        },
        removeK8sIdMap(k8s_id: string): boolean {
            return this.terminalMap.delete(k8s_id);
        }
    }
});
