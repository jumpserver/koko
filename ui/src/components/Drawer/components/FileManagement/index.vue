<script setup lang="ts">
import type { DataTableColumns } from 'naive-ui';

import { useI18n } from 'vue-i18n';
import prettyBytes from 'pretty-bytes';
import { File, Folder } from 'lucide-vue-next';
import { h, onUnmounted, ref, watch } from 'vue';
import { NFlex, NIcon, NPopover, NText } from 'naive-ui';

import { useFileManage } from '@/hooks/useFileManage.ts';
import { useFileManageStore } from '@/store/modules/fileManage.ts';

import FileManage from './widget/index.vue';

export interface RowData {
  is_dir: boolean;
  mod_time: string;
  name: string;
  perm: string;
  size: string;
  type: string;
}

const props = defineProps<{
  sftpToken: string;
  showEmpty: boolean;
}>();

const emit = defineEmits<{
  (e: 'reconnect'): void;
}>();

const { t } = useI18n();
const fileManageStore = useFileManageStore();

const isLoaded = ref(false);
const tableData = ref<RowData[]>([]);
const fileManageSocket = ref<WebSocket | undefined>(undefined);

watch(
  () => fileManageStore.fileList,
  fileList => {
    if (fileList) {
      tableData.value = fileList;
      isLoaded.value = true;
    }
  },
  {
    immediate: true,
  }
);

watch(
  () => props.sftpToken,
  token => {
    if (token) {
      fileManageSocket.value = useFileManage(token, t);
    }
  },
  { immediate: true }
);

/**
 * @description 生成表头
 */
const createColumns = (): DataTableColumns<RowData> => {
  return [
    {
      title: t('Name'),
      key: 'name',
      ellipsis: {
        tooltip: true,
      },
      render(row) {
        const fileIcon = h(NIcon, {
          size: 18,
          component: row.is_dir ? Folder : File,
          style: { marginRight: '8px' },
        });

        const fileName = h(
          NPopover,
          {
            delay: 500,
            placement: 'top-start',
            style: { maxWidth: '485px' },
          },
          {
            trigger: () =>
              h(
                NText,
                {
                  depth: 1,
                  strong: true,
                  style: {
                    cursor: 'pointer',
                    maxWidth: '200px',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  },
                },
                { default: () => row.name }
              ),
            default: () =>
              h(NText, { style: { maxWidth: '300px', wordBreak: 'break-all' } }, { default: () => row.name }),
          }
        );

        const filePermission =
          row.name !== '..' && row.perm
            ? h(
                NText,
                {
                  depth: 3,
                  style: { fontSize: '10px', marginTop: '2px' },
                },
                { default: () => row.perm }
              )
            : null;

        return h(
          NFlex,
          {
            align: 'center',
            style: { gap: '0px' },
          },
          {
            default: () => [
              fileIcon,
              h(
                NFlex,
                {
                  vertical: true,
                  style: { gap: '0px' },
                },
                {
                  default: () => [fileName, filePermission].filter(Boolean),
                }
              ),
            ],
          }
        );
      },
    },
    {
      title: t('Size'),
      key: 'size',
      align: 'center',
      width: 100,
      render(row: RowData) {
        return h(
          NText,
          {
            depth: 1,
            strong: true,
          },
          {
            default: () => prettyBytes(Number(row.size)),
          }
        );
      },
    },
  ];
};

const handleReconnect = () => {
  emit('reconnect');
};

onUnmounted(() => {
  if (fileManageSocket.value && fileManageSocket.value.readyState === WebSocket.OPEN) {
    fileManageSocket.value.close();
    fileManageSocket.value = undefined;
  }
});

const columns = createColumns();
</script>

<template>
  <template v-if="showEmpty">
    <div class=" flex flex-col items-center justify-center h-full w-full gap-4">
      <n-empty description="获取文件管理器 Token 超时" />
      <n-button type="primary" @click="handleReconnect"> {{ t('Reconnect') }} </n-button>
    </div>
  </template>

  <template v-else-if="isLoaded">
    <FileManage :columns="columns" />
  </template>

  <template v-else>
    <n-spin size="small" class="absolute w-full h-full" />
  </template>
</template>

<style scoped lang="scss">
::v-deep(.n-tabs-pane-wrapper) {
  width: 100%;
  height: 100%;
}
</style>
