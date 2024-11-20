<template>
  <n-drawer
    resizable
    v-model:show="isShowList"
    :width="settingDrawer ? 270 : 1050"
    class="transition-all duration-300 ease-in-out"
  >
    <n-drawer-content>
      <n-tabs
        type="line"
        animated
        class="w-full h-full"
        :default-value="tabDefaultValue"
        @before-leave="handleBeforeLeave"
      >
        <n-tab-pane name="setting" tab="Setting">
          <template #tab>
            <n-flex align="center" justify="flex-start">
              <n-icon size="20" :component="Settings" />
              <n-text depth="1" strong class="text-[16px]"> 设置 </n-text>
            </n-flex>
          </template>

          <template #default>
            <n-flex vertical>
              <template v-for="item of settings" :key="item.title">
                <n-button
                  v-if="!item.content"
                  quaternary
                  class="justify-start items-center"
                  :disabled="item.disabled()"
                  @click="item.click"
                >
                  <n-icon size="18" :component="item.icon" class="mr-[10px]" />
                  <n-text>
                    {{ item.title }}
                  </n-text>
                </n-button>
                <n-list class="mt-[-15px]" clickable v-else-if="item.label === 'User'">
                  <n-list-item>
                    <n-thing class="ml-[15px] mt-[10px]">
                      <template #header>
                        <n-flex align="center" justify="center">
                          <n-icon :component="item.icon" :size="18"></n-icon>
                          <n-text class="text-[14px]">
                            {{ item.title }}
                            {{ `(${item.content().length})` }}
                          </n-text>
                        </n-flex>
                      </template>
                      <template #description>
                        <n-flex size="small" style="margin-top: 4px">
                          <n-popover
                            trigger="hover"
                            placement="top"
                            v-for="detail of item.content()"
                            :key="detail.name"
                          >
                            <template #trigger>
                              <n-tag
                                round
                                strong
                                size="small"
                                class="mt-[2.5px] mb-[2.5px] mx-[25px] w-[170px] justify-around cursor-pointer overflow-hidden whitespace-nowrap text-ellipsis"
                                :bordered="false"
                                :type="item.content().indexOf(detail) !== 0 ? 'info' : 'success'"
                                :closable="true"
                                :disabled="item.content().indexOf(detail) === 0"
                                @close="item.click(detail)"
                              >
                                <n-text class="cursor-pointer text-inherit">
                                  {{ detail.name }}
                                </n-text>
                                <template #icon>
                                  <n-icon :component="detail.icon" />
                                </template>
                              </n-tag>
                            </template>
                            <template #default>
                              <span>{{ detail.tip }} {{ detail.name }}</span>
                            </template>
                          </n-popover>
                        </n-flex>
                      </template>
                    </n-thing>
                  </n-list-item>
                </n-list>
                <n-list class="mt-[-15px]" clickable v-else-if="item.label === 'Keyboard'">
                  <n-list-item>
                    <n-thing class="ml-[15px] mt-[10px]">
                      <template #header>
                        <n-flex align="center" justify="center">
                          <n-icon :component="item.icon" :size="18"></n-icon>
                          <n-text class="text-[14px]">
                            {{ item.title }}
                          </n-text>
                        </n-flex>
                      </template>
                      <template #description>
                        <n-flex size="small" style="margin-top: 4px">
                          <n-popover
                            trigger="hover"
                            placement="top"
                            v-for="detail of item.content"
                            :key="detail.name"
                          >
                            <template #trigger>
                              <n-tag
                                round
                                strong
                                type="info"
                                size="small"
                                class="mt-[2.5px] mb-[2.5px] mx-[25px] w-[170px] cursor-pointer"
                                :bordered="false"
                                :closable="false"
                                @click="detail.click()"
                              >
                                <n-text class="cursor-pointer text-inherit">
                                  {{ detail.name }}
                                </n-text>
                                <template #icon>
                                  <n-icon size="16" class="ml-[5px] mr-[5px]" :component="detail.icon" />
                                </template>
                              </n-tag>
                            </template>
                            <template #default>
                              <span>{{ detail.tip }}</span>
                            </template>
                          </n-popover>
                        </n-flex>
                      </template>
                    </n-thing>
                  </n-list-item>
                </n-list>
              </template>
            </n-flex>
          </template>
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
              <FileManage :columns="columns" />
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
// @ts-ignore
import dayjs from 'dayjs';
import mittBus from '@/utils/mittBus.ts';

import { Delete, CloudDownload } from '@vicons/carbon';
import { NButton, NFlex, NIcon, NTag, NText } from 'naive-ui';
import { Folder, Folders, Settings } from '@vicons/tabler';

import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { useFileManage } from '@/hooks/useFileManage.ts';
import { h, onBeforeUnmount, onMounted, ref, watch, unref } from 'vue';
import { useFileManageStore } from '@/store/modules/fileManage.ts';

import type { DataTableColumns } from 'naive-ui';
import type { ISettingProp } from '@/views/interface';

import FileManage from './components/fileManage/index.vue';

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
    settings: ISettingProp[];
  }>(),
  {
    settings: () => []
  }
);

const { t } = useI18n();
const message = useMessage();
const fileManageStore = useFileManageStore();

const isLoaded = ref(false);
const isShowList = ref(false);
const settingDrawer = ref(false);
const tabDefaultValue = ref('fileManage');
const tableData = ref<RowData[]>([]);

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
      align: 'center',
      render(row) {
        return h(
          NFlex,
          {
            align: 'center'
          },
          {
            default: () => [
              h(NIcon, {
                size: '16',
                component: Folder
              }),
              h(
                NText,
                {
                  depth: 1,
                  strong: true
                },
                { default: () => row.name }
              )
            ]
          }
        );
      }
    },
    {
      title: t('Date Modified'),
      key: 'mod_time',
      resizable: true,
      align: 'center',
      width: 220,
      ellipsis: {
        tooltip: true
      },
      render(row: RowData) {
        return h(
          NTag,
          {
            style: {
              marginRight: '6px'
            },
            type: 'info',
            bordered: false
          },
          { default: () => dayjs(Number(row.mod_time) * 1000).format('YYYY-MM-DD HH:mm:ss') }
        );
      }
    },
    {
      title: t('Size'),
      key: 'size',
      resizable: true,
      align: 'center',
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
      title: t('Kind'),
      key: 'type',
      resizable: true,
      align: 'center',
      render(row: RowData) {
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
      render(row: RowData) {
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

/**
 * @description 再切换 tab 标签时动态修改 drawer 的宽度
 * @param tabName
 */
const handleBeforeLeave = (tabName: string) => {
  if (tabName === 'setting') {
    settingDrawer.value = true;

    return true;
  }

  settingDrawer.value = false;

  return true;
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
