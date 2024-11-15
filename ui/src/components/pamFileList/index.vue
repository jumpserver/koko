<template>
  <n-drawer v-model:show="isShowList" :default-width="1050" resizable>
    <n-drawer-content>
      <n-tabs type="line" animated :default-value="tabDefaultValue">
        <n-tab-pane name="setting" tab="Setting">
          <template #tab>
            <n-flex align="center" justify="flex-start">
              <n-icon size="16" :component="Settings" />
              <n-text depth="1" strong class="text-[14px]"> 设置 </n-text>
            </n-flex>
          </template>

          <template #default> </template>
        </n-tab-pane>
        <n-tab-pane name="fileManage" tab="FileManage">
          <template #tab>
            <n-flex align="center" justify="flex-start">
              <n-icon size="20" :component="Folders" />
              <n-text depth="1" strong class="text-[16px]">文件管理</n-text>
            </n-flex>
          </template>

          <template #default>
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
              <n-data-table virtual-scroll :bordered="false" :data="data" :columns="columns" />
            </n-flex>
          </template>
        </n-tab-pane>
      </n-tabs>
    </n-drawer-content>
  </n-drawer>
</template>

<script setup lang="ts">
import mittBus from '@/utils/mittBus.ts';

import { Folder, Folders, Settings } from '@vicons/tabler';
import { ArrowBackIosFilled, ArrowForwardIosFilled } from '@vicons/material';
import { h, onBeforeUnmount, onMounted, ref } from 'vue';
import { NButton, NTag, useMessage } from 'naive-ui';
import type { UploadFileInfo } from 'naive-ui';

interface RowData {
  key: number;
  name: string;
  age: number;
  address: string;
  tags: string[];
}

const message = useMessage();

const isShowList = ref(false);
const isShowUploadList = ref(false);
const tabDefaultValue = ref('fileManage');

const handleOpenFileList = () => {
  isShowList.value = !isShowList.value;
};

const fileList = ref<UploadFileInfo[]>([
  {
    id: 'b',
    name: 'file.doc',
    status: 'finished',
    type: 'text/plain'
  }
]);

const handleClick = () => {
  isShowUploadList.value = true;
};

function createColumns({ sendMail }: { sendMail: (rowData: RowData) => void }): DataTableColumns<RowData> {
  return [
    {
      title: 'Name',
      key: 'name'
    },
    {
      title: 'Age',
      key: 'age',
      sorter: (row1, row2) => row1.age - row2.age
    },
    {
      title: 'Address',
      key: 'address'
    },
    {
      title: 'Tags',
      key: 'tags',
      render(row) {
        return row.tags.map(tagKey => {
          return h(
            NTag,
            {
              style: {
                marginRight: '6px'
              },
              type: 'info',
              bordered: false
            },
            {
              default: () => tagKey
            }
          );
        });
      }
    },
    {
      title: 'Action',
      key: 'actions',
      render(row) {
        return h(
          NButton,
          {
            size: 'small',
            onClick: () => sendMail(row)
          },
          { default: () => 'Send Email' }
        );
      }
    }
  ];
}

function createData(): RowData[] {
  return [
    {
      key: 0,
      name: 'John Brown',
      age: 32,
      address: 'New York No. 1 Lake Park',
      tags: ['nice', 'developer']
    },
    {
      key: 1,
      name: 'Jim Green',
      age: 42,
      address: 'London No. 1 Lake Park',
      tags: ['wow']
    },
    {
      key: 2,
      name: 'Joe Black',
      age: 32,
      address: 'Sidney No. 1 Lake Park',
      tags: ['cool', 'teacher']
    },
    {
      key: 2,
      name: 'Joe Black',
      age: 32,
      address: 'Sidney No. 1 Lake Park',
      tags: ['cool', 'teacher']
    },
    {
      key: 2,
      name: 'Joe Black',
      age: 32,
      address: 'Sidney No. 1 Lake Park',
      tags: ['cool', 'teacher']
    },
    {
      key: 2,
      name: 'Joe Black',
      age: 32,
      address: 'Sidney No. 1 Lake Park',
      tags: ['cool', 'teacher']
    },
    {
      key: 2,
      name: 'Joe Black',
      age: 32,
      address: 'Sidney No. 1 Lake Park',
      tags: ['cool', 'teacher']
    },
    {
      key: 2,
      name: 'Joe Black',
      age: 32,
      address: 'Sidney No. 1 Lake Park',
      tags: ['cool', 'teacher']
    },
    {
      key: 2,
      name: 'Joe Black',
      age: 32,
      address: 'Sidney No. 1 Lake Park',
      tags: ['cool', 'teacher']
    },
    {
      key: 2,
      name: 'Joe Black',
      age: 32,
      address: 'Sidney No. 1 Lake Park',
      tags: ['cool', 'teacher']
    },
    {
      key: 2,
      name: 'Joe Black',
      age: 32,
      address: 'Sidney No. 1 Lake Park',
      tags: ['cool', 'teacher']
    },
    {
      key: 2,
      name: 'Joe Black',
      age: 32,
      address: 'Sidney No. 1 Lake Park',
      tags: ['cool', 'teacher']
    }
  ];
}

const data = createData();
const columns = createColumns({
  sendMail(rowData) {
    message.info(`send mail to ${rowData.name}`);
  }
});

onMounted(() => {
  console.log('1');
  mittBus.on('open-fileList', handleOpenFileList);
});

onBeforeUnmount(() => {
  mittBus.off('open-fileList', handleOpenFileList);
});
</script>

<style scoped lang="scss"></style>
