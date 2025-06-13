<template>
  <template v-if="isLoaded">
    <FileManage :columns="columns" />
  </template>

  <template v-else>
    <n-spin size="small" class="absolute w-full h-full" />
  </template>
</template>

<script setup lang="ts">
import dayjs from 'dayjs';
import prettyBytes from 'pretty-bytes';
import FileManage from './fileManage/index.vue';

import { Folder } from '@vicons/tabler';
import { NEllipsis, NFlex, NIcon, NText } from 'naive-ui';

import { useI18n } from 'vue-i18n';
import { h, ref, watch, onUnmounted } from 'vue';
import { getFileName } from '@/utils';
import { useFileManage } from '@/hooks/useFileManage.ts';
import { useFileManageStore } from '@/store/modules/fileManage.ts';

import type { DataTableColumns } from 'naive-ui';
import type { ISettingProp } from '@/types';

export interface RowData {
  is_dir: boolean;
  mod_time: string;
  name: string;
  perm: string;
  size: string;
  type: string;
}

const props = withDefaults(
  defineProps<{
    showTab?: boolean;
    settings?: ISettingProp[];
    sftpToken: string;
  }>(),
  {
    settings: () => [],
    sftpToken: '',
    showTab: false
  }
);

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
    immediate: true
  }
);

watch(
  () => props.sftpToken,
  (newValue, oldValue) => {
    if (fileManageSocket.value && fileManageSocket.value.readyState === WebSocket.OPEN) {
      fileManageSocket.value.close();
    }
    if (newValue && newValue !== oldValue) {
     try {
        fileManageSocket.value = useFileManage(newValue, t);
      } catch (error) {
        console.error('Failed to initialize file management socket:', error);
        isLoaded.value = true; // 即使失败也设置加载完成，避免一直显示加载状态
      }
    }
  },
  {
    immediate: true
  }
);


// ai added to close the WebSocket connection when the component is unmounted
onUnmounted(() => {
  if (fileManageSocket.value && fileManageSocket.value.readyState === WebSocket.OPEN) {
    fileManageSocket.value.close();
    fileManageSocket.value = undefined;
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
      width: 160,
      ellipsis: {
        tooltip: true
      },
      render(row) {
        return h(
          NFlex,
          {
            align: 'center',
            style: {
              flexWrap: 'no-wrap'
            }
          },
          {
            default: () => [
              h(NIcon, {
                size: '18',
                component: Folder
              }),
              h(
                NFlex,
                {
                  justify: 'center',
                  align: 'flex-start',
                  style: {
                    flexDirection: 'column',
                    rowGap: '0px'
                  }
                },
                {
                  default: () => [
                    h(
                      NEllipsis,
                      {
                        style: {
                          maxWidth: '145px',
                          cursor: 'pointer'
                        }
                      },
                      {
                        default: () =>
                          h(
                            NText,
                            {
                              depth: 1,
                              strong: true
                            },
                            {
                              default: () => row.name
                            }
                          )
                      }
                    ),
                    h(
                      NText,
                      {
                        depth: 3,
                        strong: true,
                        style: {
                          fontSize: '10px'
                        }
                      },
                      {
                        default: () => {
                          if (row.name === '..') return;

                          return row.perm ? row.perm : '-';
                        }
                      }
                    )
                  ]
                }
              )
            ]
          }
        );
      }
    },
    {
      title: t('LastModified'),
      key: 'mod_time',
      align: 'center',
      width: 120,
      ellipsis: {
        tooltip: true
      },
      render(row: RowData) {
        return h(
          NText,
          {
            depth: 1
          },
          {
            default: () => {
              if (row.mod_time) {
                return dayjs(Number(row.mod_time) * 1000).format('YYYY-MM-DD HH:mm:ss');
              }

              return '-';
            }
          }
        );
      }
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
            strong: true
          },
          {
            default: () => prettyBytes(Number(row.size))
          }
        );
      }
    },
    {
      title: t('Type'),
      key: 'type',
      align: 'center',
      width: 100,
      render(row: RowData) {
        return h(
          NText,
          {
            depth: 1,
            strong: true
          },
          {
            default: () => getFileName(row)
          }
        );
      }
    }
  ];
};

const columns = createColumns();
</script>

<style scoped lang="scss">
::v-deep(.n-tabs-pane-wrapper) {
  width: 100%;
  height: 100%;
}
</style>
