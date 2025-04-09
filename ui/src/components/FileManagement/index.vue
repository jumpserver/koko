<template>
  <n-drawer
    resizable
    id="drawer-inner-target"
    :auto-focus="false"
    :default-width="drawerWidth"
    :min-width="drawerMinWidth"
    v-model:show="isShowList"
    v-model:width="drawerWidth"
  >
    <n-drawer-content closable :title="t('FileManagement')">
      <template v-if="isLoaded">
        <keep-alive>
          <FileManage :columns="columns" />
        </keep-alive>
      </template>

      <template v-else>
        <n-spin size="small" class="absolute w-full h-full" />
      </template>
    </n-drawer-content>
  </n-drawer>
</template>

<script setup lang="ts">
// @ts-ignore
import dayjs from 'dayjs';
import mittBus from '@/utils/mittBus.ts';
import FileManage from './components/fileManage/index.vue';

import { Folder } from '@vicons/tabler';
import { NEllipsis, NFlex, NIcon, NText } from 'naive-ui';

import { useI18n } from 'vue-i18n';
import { getFileName } from '@/utils';
import { useFileManage } from '@/hooks/useFileManage.ts';
import { useFileManageStore } from '@/store/modules/fileManage.ts';
import { h, onBeforeUnmount, onMounted, ref, watch, nextTick } from 'vue';

import type { DataTableColumns } from 'naive-ui';

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
    sftpToken: string;
  }>(),
  {
    sftpToken: ''
  }
);
const emits = defineEmits<{
  (e: 'create-file-connect-token'): void;
}>();

const { t } = useI18n();
const fileManageStore = useFileManageStore();

const isLoaded = ref(false);
const isShowList = ref(false);
const settingDrawer = ref(false);
const drawerWidth = ref(700);
const drawerMinWidth = ref(650);
const tabDefaultValue = ref('fileManage');
const tableData = ref<RowData[]>([]);
const fileManageSocket = ref<WebSocket | undefined>(undefined);

watch(
  () => fileManageStore.fileList,
  fileList => {
    if (fileList && fileList.length > 0) {
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
  token => {
    if (token) {
      fileManageSocket.value = useFileManage(token, t);
    }
  },
  {
    immediate: true
  }
);

/**
 * @description pam 中默认打开的是文件管理
 */
const handleOpenFileList = () => {
  tabDefaultValue.value = 'fileManage';
  drawerMinWidth.value = 650;
  isShowList.value = !isShowList.value;
};

/**
 * @description 处理 size
 */
const formatBytes = (bytes: string | number, decimals: number = 2): string => {
  const byteNumber = typeof bytes === 'string' ? parseInt(bytes, 10) : Number(bytes);

  if (isNaN(byteNumber) || byteNumber <= 0) return '0 Byte';

  const units = ['Byte', 'KB', 'MB', 'GB', 'TB', 'PB'];

  const i = Math.floor(Math.log2(byteNumber) / Math.log2(1024));

  return (byteNumber / Math.pow(1024, i)).toFixed(decimals) + ' ' + units[Math.min(i, units.length - 1)];
};

/**
 * @description 生成表头
 */
const createColumns = (): DataTableColumns<RowData> => {
  return [
    {
      title: t('Name'),
      key: 'name',
      width: 260,
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
                          maxWidth: '210px',
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
      width: 180,
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
            default: () => formatBytes(row.size)
          }
        );
      }
    },
    {
      title: t('Type'),
      key: 'type',
      align: 'center',
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

/**
 * @description 设置 drawer 宽度
 * @param width
 */
const adjustDrawerWidth = (width: string) => {
  nextTick(() => {
    const drawerRef: HTMLElement = document.getElementsByClassName('n-drawer')[0] as HTMLElement;

    if (drawerRef) {
      drawerRef.style.width = width;
    }
  });
};

/**
 * @description 在切换 tab 标签时动态修改 drawer 的宽度
 * @param tabName
 */
const handleBeforeLeave = (tabName: string) => {
  if (tabName === 'setting') {
    settingDrawer.value = true;
    drawerWidth.value = 270;
    drawerMinWidth.value = 270;

    return true;
  }

  if (tabName === 'fileManage') {
    settingDrawer.value = false;
    drawerWidth.value = 700;
    drawerMinWidth.value = 650;

    if (!fileManageSocket.value) {
      emits('create-file-connect-token');
    }

    return true;
  }

  return false;
};

const columns = createColumns();

onMounted(() => {
  mittBus.on('open-fileList', handleOpenFileList);
});

onBeforeUnmount(() => {
  mittBus.off('open-fileList', handleOpenFileList);
});
</script>

<style scoped lang="scss">
::v-deep(.n-tabs-pane-wrapper) {
  width: 100%;
  height: 100%;
}
</style>
