<template>
  <n-flex vertical class="w-screen h-screen !gap-y-0">
    <Header :socket="socket" :current-node-path="currentNodePath" @update:loading="handleLoading" />

    <n-split direction="horizontal" class="h-full" :max="0.5" :min="0.2" :default-size="0.2" :resize-trigger-size="1">
      <template #1>
        <n-flex vertical align="center" justify="center" class="h-full px-4 py-2 bg-[#202222]">
          <n-spin :show="loading">
            <n-tree
              block-line
              expand-on-click
              class="w-full h-full"
              :data="treeData"
              :on-load="handleLoad"
              :node-props="nodeProps"
              :default-expanded-keys="expandedKeys"
            />
          </n-spin>
        </n-flex>
      </template>
      <template #2>
        <Main :current-node-path="currentNodePath" :data="listData" />
      </template>
    </n-split>
  </n-flex>
</template>

<script setup lang="ts">
import { useRoute } from 'vue-router';
import { WINDOW_MESSAGE_TYPE } from '@/enum';
import { useFileList } from '@/hooks/useFileList';

import { ref, onMounted, onUnmounted, watch } from 'vue';
import { sendEventToLuna } from '@/components/TerminalComponent/helper';

import type { TreeOption } from 'naive-ui';

import Main from './components/Main/index.vue';
import Header from './components/Header/index.vue';

const route = useRoute();

const loading = ref(false);
const connectToken = ref('');
const currentNodePath = ref('');

if (route.query && typeof route.query.token === 'string') {
  connectToken.value = route.query.token;
}

const { socket, expandedKeys, treeData, listData, handleLoad } = useFileList(connectToken.value, 'direct');

watch(
  () => treeData,
  data => {
    if (data) {
      if (!currentNodePath.value) {
        currentNodePath.value = expandedKeys.value[0];
      }
      loading.value = false;
    }
  },
  {
    deep: true
  }
);

const handleCommunication = (event: MessageEvent) => {
  const windowMessage = event.data;

  switch (windowMessage.name) {
    case WINDOW_MESSAGE_TYPE.PING:
      sendEventToLuna(WINDOW_MESSAGE_TYPE.PONG, '', windowMessage.id, event.origin);
      break;
  }
};

const nodeProps = ({ option }: { option: TreeOption }) => {
  return {
    onClick: () => {
      currentNodePath.value = option.path as string;
    },
    onContextmenu: (e: MouseEvent) => {
      e.preventDefault();
    }
  };
};

const handleLoading = (value: boolean) => {
  loading.value = value;
};

onMounted(() => {
  window.addEventListener('message', (event: MessageEvent) => handleCommunication(event));
});

onUnmounted(() => {
  window.removeEventListener('message', handleCommunication);
});
</script>

<style scoped lang="scss">
:deep(.n-spin-container) {
  width: 100%;
  height: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;

  .n-spin-content {
    width: 100%;
    height: 100%;

    .n-tree {
      .n-tree-node-switcher {
        display: flex;
        justify-content: center;
        align-items: center;
        height: 40px;
      }

      .n-tree-node-content__text {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }

      .n-empty.n-tree__empty {
        display: flex;
        align-items: center;
        justify-content: center;
        height: 100%;
      }
    }
  }
}
</style>
