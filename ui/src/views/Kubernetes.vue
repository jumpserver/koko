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
    <MainContent />
  </n-layout>
</template>

<script setup lang="ts">
import SideTop from '@/components/Kubernetes/Sidebar/sideTop.vue';
import MainContent from '@/components/Kubernetes/MainContent/index.vue';
import FileManagement from '@/components/Kubernetes/FileManagement/index.vue';

import { ref, onMounted, onUnmounted } from 'vue';

const sideWidth = ref(300);

const handleTriggerClick = () => {
  // treeStore.changeCollapsed(!isCollapsed.value);
  // if (!isCollapsed.value) {
  //   sideWidth.value = 300;
  // } else {
  //   sideWidth.value = 0;
  // }
};

// mittBus.on('tree-click', handleTriggerClick);

onMounted(() => {
  const trigger = document.querySelector('.n-layout-toggle-button');
  if (trigger) {
    trigger.addEventListener('click', handleTriggerClick);
  }
});

onUnmounted(() => {
  const trigger = document.querySelector('.n-layout-toggle-button');
  if (trigger) {
    trigger.removeEventListener('click', handleTriggerClick);
  }
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
