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
      <FileManagement class="file-management" />
    </n-layout-sider>
    <MainContent>
      <template v-slot:terminal>
        <Terminal :enable-zmodem="true" @ws-data="wsData" />
      </template>
    </MainContent>
  </n-layout>
</template>

<script setup lang="ts">
import { useMessage } from 'naive-ui';
import SideTop from '@/components/Kubernetes/Sidebar/sideTop.vue';
import MainContent from '@/components/Kubernetes/MainContent/index.vue';
import FileManagement from '@/components/Kubernetes/FileManagement/index.vue';

import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { BASE_WS_URL } from '@/config';
import { useWebSocket } from '@vueuse/core';
import Terminal from '@/components/Terminal/Terminal.vue';

const route = useRoute();
const message = useMessage();

const sideWidth = ref(300);
const handleTriggerClick = () => {
  // treeStore.changeCollapsed(!isCollapsed.value);
  // if (!isCollapsed.value) {
  //   sideWidth.value = 300;
  // } else {
  //   sideWidth.value = 0;
  // }
};

const wsData = () => {};

// 由于在 k8s 中，是需要先通过 connect 之后才会有 tree，而组件解构
const initConnection = () => {
  const token = route.query.token!;
  const connectionURL = `${BASE_WS_URL}/koko/ws/terminal/?token=${token}`;

  // const { status, data, send, open, close } = useWebSocket(connectionURL, {
  //   onConnected: () => {
  //     console.log('WebSocket connected');
  //   },
  //   onDisconnected: () => {
  //     console.log('WebSocket disconnected');
  //   },
  //   onError: error => {
  //     console.error('WebSocket error:', error);
  //   },
  //   immediate: true,
  //   autoClose: true
  // });
};

onMounted(() => {
  initConnection();
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
