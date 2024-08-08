<template>
  <n-layout has-sider class="custom-layout">
    <n-layout-header>
      <n-flex class="header-content" vertical align="center" justify="space-between">
        <SideTop />
      </n-flex>
    </n-layout-header>
    <n-layout-sider
      v-draggable="sideWidth"
      bordered
      collapse-mode="width"
      content-style="padding: 24px;"
      :width="sideWidth"
      :collapsed-width="0"
      :collapsed="true"
      :show-collapsed-content="false"
      :native-scrollbar="false"
      class="relative transition-sider"
      :style="{
        width: sideWidth + 'px',
        maxWidth: '600px'
      }"
    >
      <!-- isCollapsed -->
      <FileManagement
        class="file-management"
        :treeNodes="treeNodes"
        @sync-load-node="handleSyncLoadNode"
      />
    </n-layout-sider>
    <MainContent>
      <template v-slot:terminal>
        <!--        <Terminal :enable-zmodem="true" @ws-data="wsData" />-->
      </template>
    </MainContent>
  </n-layout>
</template>

<script setup lang="ts">
// 使用 Hook
import { useRoute } from 'vue-router';
import { useMessage } from 'naive-ui';
import { useTree } from '@/hooks/useTree.ts';
import { useLogger } from '@/hooks/useLogger.ts';
import { useWebSocket, UseWebSocketReturn } from '@vueuse/core';
import { useWebSocket as customUseWebSocket } from '@/hooks/useWebSocket.ts';

import type { Ref } from 'vue';
import type { TreeOption } from 'naive-ui';

import SideTop from '@/components/Kubernetes/Sidebar/sideTop.vue';
import MainContent from '@/components/Kubernetes/MainContent/index.vue';
import FileManagement from '@/components/Kubernetes/FileManagement/index.vue';

// 导入 API
import { onMounted, onUnmounted, ref } from 'vue';

// 额外项
import { fireEvent } from '@/utils';
import { BASE_WS_URL } from '@/config';
import { handleError } from '@/components/Terminal/helper';

import { customTreeOption } from '@/hooks/interface';

const route = useRoute();
const message = useMessage();
const { debug } = useLogger('Kubernetes');

let socket: Ref<WebSocket | null> = ref(null);
let socketSend: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean;

let treeNodes = ref<TreeOption[]>([]);
const sideWidth = ref(300);
const connectInfo = ref<any | null>(null);
const lastReceiveTime: Ref<Date> = ref(new Date());
const terminalId = ref('');

const { onWebsocketOpen } = customUseWebSocket(terminalId);

const handleClickOutSide = () => {};
const handleSyncLoadNode = (treeNodes: customTreeOption) => {
  syncLoadNode(treeNodes, terminalId.value);
};
const handleOnMessage = (data: Ref<any>, close: WebSocket['close']) => {
  lastReceiveTime.value = new Date();

  if (data.value === undefined) {
    return;
  }

  let msg = JSON.parse(data.value);

  switch (msg.type) {
    case 'CONNECT': {
      terminalId.value = msg.id;
      connectInfo.value = JSON.parse(msg.data);
      treeNodes.value = initTree(msg.id, connectInfo.value.asset.name);

      debug('Websocket Connection Established');
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
      close();
      break;
    }
    default: {
    }
  }
};

// 由于在 k8s 中，是需要先通过 connect 之后才会有 tree，而组件解构
const initConnection = (): UseWebSocketReturn<any> => {
  const token = route.query.token!;
  const connectionURL = `${BASE_WS_URL}/koko/ws/terminal/?token=${token}`;

  const { status, data, send, open, close, ws } = useWebSocket(connectionURL, {
    onConnected: (ws: WebSocket) => {
      ws.binaryType = 'arraybuffer';
      onWebsocketOpen(status, send);
    },
    onMessage: (_ws: WebSocket, _event: MessageEvent) => {
      handleOnMessage(data, close);
    },
    onError: (_ws: WebSocket, event: Event) => {
      fireEvent(new Event('CLOSE', {}));
      handleError(event);
    },
    onDisconnected: (_ws: WebSocket, event: CloseEvent) => {
      fireEvent(new Event('CLOSE', {}));
      handleError(event);
    }
  });

  socket.value = ws.value!;
  socketSend = send;

  return {
    status,
    data,
    send,
    open,
    close,
    ws
  };
};

const { ws, send } = initConnection();
const { initTree, syncLoadNode, updateTreeNodes } = useTree(ws.value!, send);

onMounted(() => {
  document.addEventListener('click', (e: Event) => handleClickOutSide, false);
});

onUnmounted(() => {
  document.removeEventListener('click', (e: Event) => handleClickOutSide);
});
</script>

<style scoped lang="scss">
.custom-layout {
  height: 100%;

  :deep(.n-layout-scroll-container) {
    .n-layout-header {
      width: 40px;

      .header-content {
        width: 40px;
        height: 100%;
        color: #ffffff;
        background-color: #333333;
      }
    }

    .n-layout-sider {
      background-color: #252526;

      .n-scrollbar .n-scrollbar-container .n-scrollbar-content {
        padding: 0 !important;
      }

      &::after {
        position: absolute;
        top: 0;
        right: 0;
        width: 10px;
        height: 100%;
        cursor: ew-resize;
        content: '';
      }

      // 设置折叠状态下 padding 为零，否则侧边 item 图标无法点击
      &.n-layout-sider--collapsed .n-layout-sider-scroll-container {
        padding: 0 !important;
      }
    }
  }
}
</style>
