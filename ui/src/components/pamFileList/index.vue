<template>
  <n-drawer v-model:show="isShowList" :default-width="1050" resizable>
    <n-drawer-content>
      <n-tabs type="line" animated :default-value="tabDefaultValue" class="w-full h-full">
        <n-tab-pane name="setting" tab="Setting">
          <template #tab>
            <n-flex align="center" justify="flex-start">
              <n-icon size="16" :component="Settings" />
              <n-text depth="1" strong class="text-[14px]"> 设置 </n-text>
            </n-flex>
          </template>

          <template #default> </template>
        </n-tab-pane>
        <n-tab-pane name="fileManage" tab="FileManage" class="w-full h-full relative">
          <template #tab>
            <n-flex align="center" justify="flex-start">
              <n-icon size="20" :component="Folders" />
              <n-text depth="1" strong class="text-[16px]">文件管理</n-text>
            </n-flex>
          </template>

          <template #default>
            <template v-if="isLoaded">
              <n-flex align="center" justify="flex-start" class="!flex-nowrap !gap-x-10 h-[45px]">
                <n-flex class="path-part !gap-x-6 h-full" align="center">
                  <n-icon :component="ArrowBackIosFilled" size="16" class="icon-hover" />
                  <n-icon :component="ArrowForwardIosFilled" size="16" class="icon-hover" />
                </n-flex>
                <n-flex class="file-part flex-[5] h-full">
                  <n-flex class="root-node !flex-nowrap h-full" align="center" justify="center">
                    <n-icon :component="Folder" size="18" />
                    <n-text depth="1" class="text-[16px] cursor-pointer">root</n-text>
                    <n-icon :component="ArrowForwardIosFilled" size="16" />
                  </n-flex>
                  <n-flex class="file-node !flex-nowrap h-full" align="center" justify="center">
                    <n-icon :component="Folder" size="18" color="#63e2b7" />
                    <n-text depth="1" class="text-[16px] cursor-pointer">web</n-text>
                    <n-icon :component="ArrowForwardIosFilled" size="16" />
                  </n-flex>
                  <n-flex class="file-node !flex-nowrap h-full" align="center" justify="center">
                    <n-icon :component="Folder" size="18" />
                    <n-text depth="1" class="text-[16px] cursor-pointer">new</n-text>
                  </n-flex>
                </n-flex>
                <n-flex class="upload-part" align="center" justify="center">
                  <n-upload
                    abstract
                    :default-file-list="fileList"
                    action="https://www.mocky.io/v2/5e4bafc63100007100d8b70f"
                  >
                    <n-button-group>
                      <n-upload-trigger #="{ handleClick }" abstract>
                        <n-button
                          secondary
                          round
                          type="primary"
                          size="small"
                          @click="
                            () => {
                              handleClick();
                              isShowUploadList = !isShowUploadList;
                            }
                          "
                        >
                          上传文件
                        </n-button>
                      </n-upload-trigger>
                    </n-button-group>
                    <n-card
                      v-if="isShowUploadList"
                      closable
                      title="文件列表"
                      class="absolute top-[3.5rem] right-2 z-[999999] w-[500px] h-[300px]"
                    >
                      <n-upload-file-list />
                    </n-card>
                  </n-upload>
                </n-flex>
              </n-flex>

              <n-divider class="!my-[12px]" />

              <n-flex class="table-part">
                <n-data-table
                  virtual-scroll
                  :bordered="false"
                  :columns="columns"
                  :max-height="1150"
                  :data="fileManageStore.fileList"
                />
              </n-flex>
            </template>

            <template v-else>
              <n-spin size="small" class="absolute w-full h-full" />
            </template>
          </template>
        </n-tab-pane>
      </n-tabs>
    </n-drawer-content>
  </n-drawer>
</template>

<script setup lang="ts">
import mittBus from '@/utils/mittBus.ts';

import { Delete, CloudDownload } from '@vicons/carbon';
import { NButton, NIcon, NTag, NText } from 'naive-ui';
import { Folder, Folders, Settings } from '@vicons/tabler';
import { ArrowBackIosFilled, ArrowForwardIosFilled } from '@vicons/material';

import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { useFileManage } from '@/hooks/useFileManage.ts';
import { h, onBeforeUnmount, onMounted, ref, watch, unref } from 'vue';
import { useFileManageStore } from '@/store/modules/fileManage.ts';

import type { UploadFileInfo, DataTableColumns } from 'naive-ui';

interface RowData {
  is_dir: boolean;
  mod_time: string;
  name: string;
  perm: string;
  size: string;
  type: string;
}

const { t } = useI18n();
const message = useMessage();
const fileManageStore = useFileManageStore();

const isLoaded = ref(false);
const isShowList = ref(false);
const isShowUploadList = ref(false);
const tabDefaultValue = ref('fileManage');

const tableData = ref<RowData[]>([]);
const fileList = ref<UploadFileInfo[]>([
  {
    id: 'b',
    name: 'file.doc',
    status: 'finished',
    type: 'text/plain'
  }
]);

const actionItem = [
  {
    iconName: Delete,
    label: t('Delete'),
    type: 'error',
    onClick: (row: RowData) => {
      message.success(row.name);
    }
  },
  {
    iconName: CloudDownload,
    label: t('Download'),
    type: 'info',
    onClick: (row: RowData) => {
      message.success(row.name);
    }
  }
];

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

const handleOpenFileList = () => {
  isShowList.value = !isShowList.value;

  useFileManage();
};

/**
 * @description 处理 size
 */
const formatBytes = (bytes: string | number, decimals?: number): string => {
  if (typeof bytes === 'string') {
    bytes = parseInt(bytes, 10);
  }

  if (bytes <= 0) return '0 Byte';

  const units = ['Byte', 'KB', 'MB', 'GB', 'TB', 'PB'];

  const i = Math.floor(Math.log(bytes) / Math.log(1024));

  return (bytes / Math.pow(1024, i)).toFixed(decimals) + ' ' + units[i];
};

/**
 * @description 处理文件名称
 * @param row
 */
const getFileName = (row: RowData) => {
  if (row.is_dir) {
    return 'Folder';
  }

  return row.name.split('.')[1];
};

/**
 * @description 生成表头
 */
const createColumns = (): DataTableColumns<RowData> => {
  return [
    {
      title: t('Name'),
      key: 'name',
      resizable: true,
      align: 'center'
    },
    {
      title: t('Date Modified'),
      key: 'mod_time',
      resizable: true,
      align: 'center',
      width: 220,
      ellipsis: {
        tooltip: true
      }
    },
    {
      title: t('Size'),
      key: 'size',
      resizable: true,
      align: 'center',
      render(row) {
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
      title: t('Kind'),
      key: 'type',
      resizable: true,
      align: 'center',
      render(row) {
        return h(
          NTag,
          {
            style: {
              marginRight: '6px'
            },
            type: 'success',
            bordered: false
          },
          {
            default: () => getFileName(row)
          }
        );
      }
    },
    {
      title: t('Actions'),
      key: 'actions',
      resizable: true,
      align: 'center',
      render(row) {
        return actionItem.map(item => {
          return h(
            NButton,
            {
              strong: true,
              text: true,
              size: 'small',
              // @ts-ignore
              type: item.type,
              style: {
                margin: '0 10px'
              },
              onClick: () => item.onClick(row)
            },
            {
              default: () => [
                h(NIcon, {
                  size: 16,
                  component: unref(item.iconName)
                })
              ]
            }
          );
        });
      }
    }
  ];
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

::v-deep(.n-data-table-td--last-col) {
  line-height: 100% !important;
}
</style>
