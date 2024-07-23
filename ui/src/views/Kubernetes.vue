<template>
  <div class="tab-layout">
    <div class="left-tree">
      <div class="treebox">
        <ul
            id="k8sTree"
            class="ztree"
        />
      </div>
      <div id="contextMenu" class="context-menu" v-if="contextMenuVisible"
           :style="{ top: `${contextMenuPosition.y}px`, left: `${contextMenuPosition.x}px` }">
        <ul>
          <li @click="createTab">connect</li>
        </ul>
      </div>
    </div>
    <div class="right">
      <el-tabs v-model="activeTabName" type="card" @tab-remove="removeTab">
        <el-tab-pane
            v-for="tab in tabs"
            :key="tab.name"
            :label="tab.label"
            :name="tab.name"
            closable
        >
          <KubernetesTerminal
              :enable-zmodem='true'
              :ws="ws"
              :connectInfo="connectInfo"
              :k8sId="tab.name"
              :namespace="tab.namespace"
              :pod="tab.pod"
              :container="tab.container"
              :messages="messages[tab.name]"
              @send-data="sendDataToWs"/>
        </el-tab-pane>
      </el-tabs>
    </div>
  </div>
</template>

<script>
import $ from 'jquery'
import '@ztree/ztree_v3/js/jquery.ztree.all.min.js'
import '@ztree/ztree_v3/js/jquery.ztree.exhide.min.js'
import '@/styles/ztree.css'
import '@/styles/ztree_icon.scss'

import KubernetesTerminal from '@/components/KubernetesTerminal.vue';
import {BASE_WS_URL, fireEvent} from "@/utils/common";
import { v4 as uuidv4 } from 'uuid';

const MaxTimeout = 30 * 1000

export default {
  components: {
    KubernetesTerminal
  },
  data() {
    return {
      zTreeSetting: {
        view: {
          dblClickExpand: false,
          showLine: true,
          fontCss: (treeId, treeNode) => {
            if (treeNode.chkDisabled) {
              return {opacity: '0.4'};
            }
            return {};
          }
        },
        data: {
          simpleData: {
            enable: true
          },
          key: {
            title: 'title'
          }
        },
        callback: {
          onExpand: this.onNodeClick,
          onRightClick: this.handleNodeRightClick,
        }
      },
      activeTabName: 'tab0',
      tabs: [],
      contextMenuVisible: false,
      contextMenuPosition: {x: 0, y: 0},
      currentNode: null,
      ws: null,
      terminalId: null,
      connectInfo: {},
      pingInterval: null,
      lastSendTime: null,
      lastReceiveTime: null,
      messages: {}
    };
  },
  methods: {
    onNodeClick(event, treeId, treeNode) {
      if (!treeNode.children && !treeNode.loaded) {
        this.loadChildNodes(treeNode, treeId);
      }
    },
    loadChildNodes(treeNode, treeId) {
      console.debug('Load child nodes:', treeNode, treeId)
      const data = {
        namespace: treeNode?.namespace || '',
        pod: treeNode?.pod || '',
        type: 'TERMINAL_K8S_TREE',
        k8s_id: treeNode.id
      }

      this.sendDataToWs(data);
      treeNode.loaded = true;
    },

    findTreeNode(id) {
      const zTree = $.fn.zTree.getZTreeObj('k8sTree');
      return zTree.getNodeByParam('id', id, null);
    },

    updateTreeNodes(msg) {
      const nodeID = msg.k8s_id;
      const treeNode = this.findTreeNode(nodeID);
      const zTree = $.fn.zTree.getZTreeObj('k8sTree');

      if (!treeNode) {
        console.error('Tree node not found:', nodeID);
        return;
      }

      if (!zTree) {
        console.error('ZTree object not found for:', 'k8sTree');
        return;
      }
      let data = JSON.parse(msg.data)
      const childNodes = data.map(name => {
        const baseNode = {
          id: `${nodeID}-${name}`,
          name: name,
          isParent: true
        };

        if (treeNode?.pod) {
          return { ...baseNode, namespace: treeNode.namespace, pod: treeNode.pod, container: name, isParent: false };
        } else if (treeNode?.namespace) {
          return { ...baseNode, namespace: treeNode.namespace, pod: name };
        } else {
          return { ...baseNode, namespace: name };
        }
      });
      zTree.addNodes(treeNode, childNodes);
      treeNode.loaded = true;
    },

    handleNodeRightClick(event, data, node) {
      console.log('Right click node:', node);
      if (!node?.container && node?.id !== this.terminalId) return;
      event.preventDefault();
      this.contextMenuVisible = true;
      this.contextMenuPosition = {x: event.clientX, y: event.clientY};
      this.currentNode = node;
    },

    createTab() {
      if (this.currentNode) {
        this.addTab(this.currentNode);
      }
      this.contextMenuVisible = false;
    },

    addTab(node) {
      const k8sID = uuidv4();
      console.debug('Add tab:', node.name, k8sID);
      this.tabs.push({
        label: node.name,
        name: k8sID,
        namespace: node.namespace,
        pod: node.pod,
        container: node.container,
        url: 'about:blank'
      });
      this.activeTabName = k8sID;
      this.$set(this.messages, k8sID, '');
    },

    removeTab(targetName) {
      const tabs = this.tabs;
      let activeName = this.activeTabName;
      if (activeName === targetName) {
        tabs.forEach((tab, index) => {
          if (tab.name === targetName) {
            const nextTab = tabs[index + 1] || tabs[index - 1];
            if (nextTab) {
              activeName = nextTab.name;
            }
          }
        });
      }
      this.activeTabName = activeName;
      this.tabs = tabs.filter(tab => tab.name !== targetName);
      this.$delete(this.messages, targetName);
    },

    handleClickOutside(event) {
      const contextMenu = this.$refs.contextMenu;
      if (contextMenu && !contextMenu.contains(event.target)) {
        this.contextMenuVisible = false;
      }
    },

    sendDataToWs(data) {
      this.lastSendTime = new Date();
      data.id = this.terminalId;
      if (this.wsIsActivated()) {
        this.ws.send(JSON.stringify(data));
      }
    },

    connect() {
      console.log('Connect to websocket');
      const urlParams = new URLSearchParams(window.location.search.slice(1));
      const connectURL = `${BASE_WS_URL}/koko/ws/terminal/?${urlParams.toString()}`;
      this.$log.debug(connectURL)

      if (this.wsIsActivated()) {
        this.ws.onerror = () => {
        };
        this.ws.onclose = () => {
        };
        this.ws.onmessage = () => {
        };
        this.ws.close();
      }

      const ws = new WebSocket(connectURL, ["JMS-KOKO"]);
      this.ws = ws;

      ws.binaryType = "arraybuffer";
      ws.onopen = this.onWebsocketOpen;
      ws.onerror = this.onWebsocketErr;
      ws.onclose = this.onWebsocketClose;
      ws.onmessage = this.handleWebSocketMessage;
    },

    wsIsActivated() {
      if (this.ws) {
        return !(this.ws.readyState === WebSocket.CLOSING ||
            this.ws.readyState === WebSocket.CLOSED)
      }
      return false
    },

    handleWebSocketMessage(e) {
      this.lastReceiveTime = new Date();
      if (e.data === undefined) {
        return
      }

      let msg = JSON.parse(e.data)
      switch (msg.type) {
        case 'CONNECT': {
          this.terminalId = msg.id;
          this.connectInfo = JSON.parse(msg.data);
          $.fn.zTree.init($('#k8sTree'), this.zTreeSetting, [
            {id: msg.id, name: this.connectInfo.asset.name, isParent: true},
          ])
          this.$log.debug("Websocket connection established")
          break
        }
        case 'TERMINAL_K8S_TREE': {
          this.updateTreeNodes(msg);
          break
        }
        case 'PING': {
          break
        }
        case "CLOSE":
        case'ERROR':
          alert('Receive Connection closed');
          this.ws.close();
          break
        default:
          this.$set(this.messages, msg.k8s_id, msg);
      }
    },

    onWebsocketOpen() {
      if (this.pingInterval !== null) {
        clearInterval(this.pingInterval);
      }
      this.lastReceiveTime = new Date();
      this.pingInterval = setInterval(() => {
        if (this.ws.readyState === WebSocket.CLOSING ||
            this.ws.readyState === WebSocket.CLOSED) {
          clearInterval(this.pingInterval)
          return
        }
        let currentDate = new Date();
        if ((this.lastReceiveTime - currentDate) > MaxTimeout) {
          this.$log.debug("more than 30s do not receive data")
        }
        let pingTimeout = (currentDate - this.lastSendTime) - MaxTimeout
        if (pingTimeout < 0) {
          return;
        }
        this.sendDataToWs({
          type: 'PING',
          data: ''
        })
      }, 25 * 1000);
    },

    onWebsocketErr(e) {
      console.log("Connection websocket error");
      fireEvent(new Event("CLOSE", {}))
      this.handleError(e)
    },

    onWebsocketClose(e) {
      console.log("Connection websocket closed");
      fireEvent(new Event("CLOSE", {}))
      this.handleError(e)
    }
  },
  mounted() {
    this.connect();
    document.addEventListener('click', this.handleClickOutside);
  },
  beforeDestroy() {
    if (this.ws) {
      this.ws.close();
    }
    document.removeEventListener('click', this.handleClickOutside);
  }
};
</script>

<style lang="scss" scoped>
.treebox {
  display: flex;
  flex-direction: column;
  justify-content: flex-start;
  padding: 10px 10px 0 10px;
  background-color: #2f2a2a;

  .ztree {
    width: 100%;
    overflow: auto;
    height: 648px;
    background-color: #2f2a2a;

    .level0 {
      .node_name {
        max-width: 120px;
        text-overflow: ellipsis;
        overflow: hidden;
      }
    }

    li {
      background-color: transparent !important;

      .button {
        background-color: rgba(0, 0, 0, 0);
      }

      ul {
        background-color: transparent !important;
      }
    }
  }
}

.dataTables_wrapper .dataTables_processing {
  opacity: .9;
  border: none;
}

.ztree ::v-deep .fa {
  font: normal normal normal 14px/1 FontAwesome !important;
}

::v-deep .tree-banner-icon-zone {
  position: absolute;
  right: 7px;
  height: 30px;
  overflow: hidden;

  .fa {
    color: #838385 !important;;

    &:hover {
      color: #606266 !important;;
    }
  }
}

.tab-layout {
  display: flex;
  height: 100vh;
}

.left-tree {
  width: 20%;
  overflow-y: auto;
  border-right: 1px solid #1f1b1b;
  position: relative;
}

.right {
  width: 80%;
  display: flex;
  flex-direction: column;
  background-color: #1f1b1b;
}

.el-tabs__content {
  height: 100%;
}

.el-tabs.el-tabs--card.el-tabs--top {
  background-color: #2f2a2a;
}

.el-tabs--card>.el-tabs__header {
  background-color: #2f2a2a;
}

.context-menu {
  position: absolute;
  z-index: 9999;
  background: white;
  border: 1px solid #ccc;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.1);
}

.context-menu ul {
  list-style: none;
  padding: 10px;
  margin: 0;
}

.context-menu ul li {
  padding: 5px;
  cursor: pointer;
}

.context-menu ul li:hover {
  background: #eee;
}
</style>
