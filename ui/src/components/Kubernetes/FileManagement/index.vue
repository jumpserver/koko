<template>
  <div>
    <n-descriptions label-placement="top" class="tree-wrapper">
      <template #header>
        <n-flex align="center" justify="space-between">
          {{ t('List of Assets') }}
          <n-icon size="16px" :component="EllipsisHorizontal" class="mr-2.5 cursor-pointer" />
        </n-flex>
      </template>
      <n-descriptions-item class="h-full">
        <n-collapse arrow-placement="left" :default-expanded-names="['asset-tree']">
          <n-collapse-item title="Kubernetes" class="collapse-item" name="asset-tree">
            <n-tree
              draggable
              block-line
              block-node
              check-on-click
              expand-on-click
              class="tree-item"
              checkbox-placement="left"
              :data="testData"
              :pattern="pattern"
              :show-line="true"
              :node-props="nodeProps"
              :on-update:expanded-keys="updatePrefixWithExpaned"
            />
          </n-collapse-item>
        </n-collapse>
      </n-descriptions-item>
    </n-descriptions>

    <!-- 右键菜单	-->
    <n-dropdown
      trigger="manual"
      placement="bottom-start"
      :show="showDropdownRef"
      :options="optionsRef as any"
      :x="xRef"
      :y="yRef"
      @select="handleSelect"
      @clickoutside="handleClickoutside"
    />
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { reactive, ref, h, onUnmounted, onMounted } from 'vue';
// import { getTreeSource, getTreeDetailById } from '@/API/modules/tree';
// import ConnectionDialog from '@/components/ConnectionDialog/index.vue';
import { NIcon, TreeOption, DropdownOption, useDialog, NPopover } from 'naive-ui';
import {
  Folder,
  FolderOpenOutline,
  FileTrayFullOutline,
  EllipsisHorizontal
} from '@vicons/ionicons5';

// import type { Tree } from '@/API/interface';

const { t } = useI18n();
const dialog = useDialog();
// const treeStore = useTreeStore();
// const { isAsync } = storeToRefs(treeStore);

const pattern = ref('');
const showDialog = ref(false);

let testData = ref<TreeOption[]>([]);

const showDropdownRef = ref(false);
const optionsRef = ref<DropdownOption[]>([]);
const xRef = ref(0);
const yRef = ref(0);

const updatePrefixWithExpaned = (
  _keys: Array<string | number>,
  _option: Array<TreeOption | null>,
  meta: {
    node: TreeOption | null;
    action: 'expand' | 'collapse' | 'filter';
  }
) => {
  if (!meta.node) return;
  switch (meta.action) {
    case 'expand':
      meta.node.prefix = () =>
        h(NIcon, null, {
          default: () => h(FolderOpenOutline)
        });
      break;
    case 'collapse':
      meta.node.prefix = () =>
        h(NIcon, null, {
          default: () => h(Folder)
        });
      break;
  }
};
const nodeProps = ({ option }: { option: TreeOption }) => {
  return {
    onClick: async () => {
      const { id } = option;

      // todo)) 只有资产才能点击
      try {
        if (id) {
          // const res = await getTreeDetailById(id as string);

          // console.log('res', res);

          dialog.success({
            showIcon: false,
            closeOnEsc: false,
            closable: true,
            autoFocus: true,
            // title: `${t('Connect')} - ${res.name}`,
            // content: () =>
            // h(ConnectionDialog, {
            //   id: res.id,
            //   permedAccounts: res.permed_accounts,
            //   permedProtocols: res.permed_protocols
            // }),
            style: {
              width: 'auto'
            }
          });
          showDialog.value = true;
        }
      } catch (e) {
        console.log(e);
      }
    },
    onContextmenu(e: MouseEvent): void {
      optionsRef.value = [option];
      showDropdownRef.value = true;
      xRef.value = e.clientX;
      yRef.value = e.clientY;
      console.log(e.clientX, e.clientY);
      e.preventDefault();
    },
    render: () => {
      return h(
        NPopover,
        {
          trigger: 'hover',
          content: option.label
        },
        {
          default: () =>
            h(
              'div',
              {
                class: 'tree-node-content'
              },
              [option.label]
            )
        }
      );
    }
  };
};

// todo)) 由于会出现同一个资产挂载到不同的父节点上的情况，此时点击会将两个资产一同点击，因此不能单纯的拿 id 作为 key，
// todo)) 对于异步加载需要额外处理添加 on-load
const loadTree = async (isAsync: Boolean) => {
  try {
    // 默认异步加载资产树
    // const res: Tree[] = await getTreeSource(isAsync);
    const treeMap: { [key: string]: TreeOption } = {};

    // res.forEach(node => {
    //   treeMap[node.id] = {
    //     key: node.id,
    //     label: node.name,
    //     prefix: () =>
    //       h(NIcon, null, {
    //         default: () => h(node.isParent ? Folder : FileTrayFullOutline)
    //       }),
    //     children: [],
    //     ...node
    //   };
    // });

    // res.forEach(node => {
    //   if (node.pId && treeMap[node.pId]) {
    //     treeMap[node.pId]?.children?.push(treeMap[node.id]);
    //   }
    // });

    const data = Object.values(treeMap).filter(node => !node.pId);

    testData.value = data;
  } catch (e) {
    console.log(e);
  }
};

const handleSelect = () => {
  showDropdownRef.value = false;
};

const handleClickoutside = () => {
  showDropdownRef.value = false;
};

onMounted(async () => {
  // await loadTree(isAsync.value);
});

// mittBus.on('tree-load', () => {
//   loadTree(isAsync.value);
// });
//
// onUnmounted(() => {
//   mittBus.off('tree-load');
// });
</script>

<style scoped lang="scss">
.tree-wrapper {
  height: 100%;
  overflow: hidden;

  :deep(.n-descriptions-header) {
    height: 35px;
    margin-bottom: unset;
    margin-left: 24px;
    font-size: 11px;
    font-weight: 400;
    line-height: 40px;
    color: var(--el-aside-tree-head-title-color);
  }

  :deep(.n-descriptions-table-wrapper) {
    height: calc(100vh - 35px);
  }

  .collapse-item {
    margin: 0;
    height: 100%;

    :deep(.n-collapse-item__header) {
      padding-top: 0;

      .n-collapse-item__header-main {
        height: 22px;
        margin-left: 5px;
      }
    }

    :deep(.n-collapse-item__content-wrapper) {
      margin-left: 16px;

      .n-collapse-item__content-inner {
        padding-top: 0;

        .tree-item .n-tree-node-wrapper {
          padding: unset;
          line-height: 22px;

          .n-tree-node-content {
            padding: 0 6px 0 0;
          }
        }
      }
    }
  }
}
</style>
