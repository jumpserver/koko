<template>
  <n-flex vertical class="w-screen h-screen !gap-y-0">
    <Header
      :socket="socket"
      :initial-path="initial_path"
      :current-node-path="currentNodePath"
      @update:layout="handleChangeLayout"
      @update:reloading="handleReloading"
    />

    <n-split direction="horizontal" class="h-full" :max="0.5" :min="0.2" :default-size="0.2" :resize-trigger-size="1">
      <template #1>
        <n-flex vertical align="center" justify="center" class="h-full px-4 py-2 bg-[#202222]">
          <n-spin :show="loading">
            <n-tree
              block-line
              class="w-full h-full"
              :data="treeData"
              :on-load="handleLoad"
              :node-props="nodeProps"
              :expanded-keys="expandedKeys"
              @update:expanded-keys="expandedKeys = $event"
            />
          </n-spin>
        </n-flex>
      </template>
      <template #2>
        <Main
          :data="listData"
          :is-grid="isGrid"
          :current-node-path="currentNodePath"
          @enter-file="handleEnterFile"
          @back="handleFileBack"
        />
      </template>
    </n-split>
  </n-flex>
</template>

<script setup lang="ts">
import { v4 as uuid } from 'uuid';
import { useRoute } from 'vue-router';
import { useMessage } from 'naive-ui';
import { WINDOW_MESSAGE_TYPE } from '@/enum';
import { useFileList } from '@/hooks/useFileList';
import { SFTP_CMD, FILE_MANAGE_MESSAGE_TYPE } from '@/enum';

import { ref, onMounted, onUnmounted, watch } from 'vue';
import { sendEventToLuna } from '@/components/TerminalComponent/helper';

import type { TreeOption } from 'naive-ui';

import Main from './components/Main/index.vue';
import Header from './components/Header/index.vue';

const route = useRoute();
const message = useMessage();

const isGrid = ref(false);
const loading = ref(false);
const connectToken = ref('');
const currentNodePath = ref('');

if (route.query && typeof route.query.token === 'string') {
  connectToken.value = route.query.token;
}

const { socket, expandedKeys, treeData, listData, initial_path, handleLoad } = useFileList(
  connectToken.value,
  'direct'
);

watch(
  () => treeData,
  data => {
    if (data.length > 0) {
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

// 监听当前路径的变化，确保对应的节点在树中被展开
watch(
  () => currentNodePath.value,
  newPath => {
    if (!newPath) return;

    const pathParts = newPath.split('/').filter(part => part !== '');
    const pathsToExpand = [];

    let currentPath = '';

    // 收集所有需要展开的路径
    for (const part of pathParts) {
      currentPath = currentPath ? `${currentPath}/${part}` : `/${part}`;
      pathsToExpand.push(currentPath);
    }

    // 只添加尚未展开的路径
    const newPathsToExpand = pathsToExpand.filter(path => !expandedKeys.value.includes(path));

    if (newPathsToExpand.length > 0) {
      expandedKeys.value = [...expandedKeys.value, ...newPathsToExpand];
    }
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
      if (option.is_dir) {
        // 只有在当前节点是目录时才更新路径
        currentNodePath.value = option.path as string;

        // 手动管理展开状态
        const key = option.key as string;

        if (expandedKeys.value.includes(key)) {
          // 如果已经展开，我们保持展开状态
          expandedKeys.value = expandedKeys.value.filter(k => k !== key);
        } else {
          // 如果未展开，则添加到展开列表
          expandedKeys.value = [...expandedKeys.value, key];
        }
      } else {
        // 非目录节点的处理
        currentNodePath.value = option.path as string;
      }
    },
    onContextmenu: (e: MouseEvent) => {
      e.preventDefault();
    }
  };
};

const handleReloading = (value: boolean) => {
  loading.value = value;

  const sendData = {
    path: currentNodePath
  };

  const sendBody = {
    id: uuid(),
    cmd: SFTP_CMD.LIST,
    type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
    data: JSON.stringify(sendData)
  };

  if (socket.value) {
    socket.value.send(JSON.stringify(sendBody));
  }
};

const handleChangeLayout = (value: boolean) => {
  isGrid.value = value;
};

const handleEnterFile = (filePath: string) => {
  // 只处理目录的点击事件
  const fileItem = listData.find(item => `${currentNodePath.value}/${item.name}` === filePath);

  if (fileItem && fileItem.is_dir) {
    // 将当前节点路径设置为新的文件路径
    currentNodePath.value = filePath;

    const sendBody = {
      id: uuid(),
      type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
      cmd: SFTP_CMD.LIST,
      data: JSON.stringify({ path: filePath })
    };

    if (socket.value) {
      socket.value.send(JSON.stringify(sendBody));
      loading.value = true;
    }
  } else if (fileItem && !fileItem.is_dir) {
    message.info('暂未支持文件预览');
  }
};

const handleFileBack = (filePath: string) => {
  currentNodePath.value = filePath;

  const sendBody = {
    id: uuid(),
    type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
    cmd: SFTP_CMD.LIST,
    data: JSON.stringify({ path: filePath })
  };

  if (socket.value) {
    socket.value.send(JSON.stringify(sendBody));
  }
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
