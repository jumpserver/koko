<script setup lang="ts">
import type { DataTableColumns } from 'naive-ui';

import dayjs from 'dayjs';
import { useI18n } from 'vue-i18n';
import { h, watchEffect } from 'vue';
import prettyBytes from 'pretty-bytes';
import { File, Folder } from 'lucide-vue-next';
import { NFlex, NIcon, NPopover, NTag, NText } from 'naive-ui';

import { useFileOperation } from '@/hooks/useFileOperation';

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
const { createFileSocket, fileList } = useFileOperation();

watchEffect(() => {
  if (props.sftpToken) {
    createFileSocket(props.sftpToken);
  }
});

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
      resizable: true,
      maxWidth: 320,
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

        return h(
          NFlex,
          {
            align: 'center',
            style: { gap: '0px', flexWrap: 'nowrap' },
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
                  default: () => [fileName],
                }
              ),
            ],
          }
        );
      },
    },
    {
      title: '权限',
      key: 'perm',
      align: 'center',
      width: 120,
      render(row: RowData) {
        let type: 'default' | 'info' | 'success' | 'warning' | 'error' = 'default';

        if (row.perm.startsWith('d')) {
          type = 'info';
        } else if (row.perm.startsWith('-')) {
          type = 'success';
        } else if (row.perm.startsWith('s')) {
          type = 'warning';
        } else if (row.perm.includes('lock')) {
          type = 'error';
        } else {
          type = 'error';
        }

        return h(NTag, { type, round: true, size: 'small', bordered: false }, { default: () => row.perm });
      },
    },
    {
      title: '修改时间',
      key: 'mod_time',
      align: 'center',
      width: 180,
      render(row: RowData) {
        return h(
          NText,
          { depth: 1, strong: true },
          { default: () => dayjs(Number(row.mod_time) * 1000).format('YYYY-MM-DD HH:mm:ss') }
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

const columns = createColumns();
</script>

<template>
  <template v-if="showEmpty">
    <div class="flex flex-col items-center justify-center h-full w-full gap-4">
      <n-empty :description="t('FileManagerTokenTimeout')" />
      <n-button secondary size="small" @click="handleReconnect">
        {{ t('Reconnect') }}
      </n-button>
    </div>
  </template>

  <template v-else>
    <FileManage :columns="columns" :file-list="fileList" />
  </template>
</template>
