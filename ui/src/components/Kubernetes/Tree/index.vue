<template>
  <div class="group">
    <n-descriptions label-placement="top" class="tree-wrapper">
      <template #header>
        <n-flex align="center" justify="space-between">
          {{ t('KubernetesManagement') }}
        </n-flex>
      </template>
      <n-descriptions-item class="h-full">
        <n-collapse
          arrow-placement="left"
          :accordion="true"
          :default-expanded-names="['asset-tree']"
        >
          <n-scrollbar style="max-height: calc(100vh - 100px)">
            <n-collapse-item
              :title="root?.label"
              class="collapse-item"
              name="asset-tree"
            >
              <template #header-extra>
                <n-flex
                  justify="center"
                  style="gap: 8px 5px !important"
                  class="mr-[10px] !hidden group-hover:!flex"
                >
                  <template v-for="option of buttonGroups" :key="option.label">
                    <n-popover>
                      <template #trigger>
                        <n-button
                          text
                          class="w-[20px] h-[20px] p-[2px] hover:!bg-[#5A5D5E4F] rounded-[5px]"
                          @click="
                            (e: Event) => {
                              option.click(e);
                            }
                          "
                        >
                          <n-icon size="15" :component="option.icon" />
                        </n-button>
                      </template>
                      {{ option.label }}
                    </n-popover>
                  </template>
                </n-flex>
              </template>
              <n-input
                clearable
                size="small"
                placeholder="搜索"
                class="mb-[3px] pl-[4px]"
                v-if="showSearch"
                v-model:value="searchPattern"
              />
              <n-tree
                cascade
                show-line
                block-node
                block-line
                expand-on-click
                class="tree-item"
                check-strategy="all"
                checkbox-placement="left"
                :data="treeNodes"
                :node-props="nodeProps"
                :pattern="searchPattern"
                :render-label="showToolTip"
                :expanded-keys="expandedKeysRef"
                :allow-checking-not-loaded="true"
                :on-update:expanded-keys="handleExpandCollapse"
              >
                <template #empty>
                  <template v-if="!isLoaded">
                    <n-spin size="small" class="w-full pt-[20px]" />
                  </template>
                  <template v-else>
                    <n-empty />
                  </template>
                </template>
              </n-tree>
              <!-- :on-load="useDebounceFn(handleOnLoad, 300)" -->
            </n-collapse-item>
          </n-scrollbar>
        </n-collapse>
      </n-descriptions-item>
    </n-descriptions>

    <!-- Context Menu -->
    <n-dropdown
      trigger="manual"
      placement="bottom"
      :show="showDropdown"
      :show-arrow="true"
      :options="dropdownOptions"
      :x="dropdownX"
      :y="dropdownY"
      @select="handleSelect"
      @clickoutside="handleClickOutside"
    />
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { storeToRefs } from 'pinia';
import { ref, h, nextTick, watchEffect } from 'vue';
import { showToolTip } from '../helper/index';
import { useTreeStore } from '@/store/modules/tree.ts';

import mittBus from '@/utils/mittBus';

import { Folder, FolderOpen } from '@vicons/fa';
import { RefreshRound } from '@vicons/material';
import { ExpandCategories } from '@vicons/carbon';
import { Terminal2, Search } from '@vicons/tabler';
import { NIcon, TreeOption, DropdownOption } from 'naive-ui';

const { t } = useI18n();
const treeStore = useTreeStore();

const { treeNodes, root } = storeToRefs(treeStore);

const emits = defineEmits<{
  (e: 'sync-load-node', data?: TreeOption): void;
  (e: 'reload-tree'): void;
}>();

const dropdownY = ref(0);
const dropdownX = ref(0);
const searchPattern = ref('');
const isLoaded = ref(false);
const showSearch = ref(false);
const showDropdown = ref(false);
const currentNodeInfo = ref();
const expandedKeysRef = ref<string[]>([]);
const dropdownOptions = ref<DropdownOption[]>([]);

const allOptions = [
  {
    label: t('Expand'),
    key: 'expand',
    icon: () => h(NIcon, { size: 15 }, { default: () => h(ExpandCategories) })
  },
  {
    label: t('Connect'),
    key: 'connect',
    icon: () => h(NIcon, { size: 15 }, { default: () => h(Terminal2) })
  }
];
const buttonGroups = [
  {
    label: t('Connect'),
    icon: Terminal2,
    click: (e: Event) => {
      handleRootLink(e);
    }
  },
  {
    label: t('Search'),
    icon: Search,
    click: (e: Event) => {
      e.stopPropagation();
      showSearch.value = !showSearch.value;
    }
  },
  {
    label: t('Refresh'),
    icon: RefreshRound,
    click: (e: Event) => {
      e.stopPropagation();
      isLoaded.value = false;
      emits('reload-tree');
    }
  }
];

watchEffect(() => {
  if (treeNodes.value.length > 0) {
    isLoaded.value = true;
  }
});

/**
 * @description 处理节点展开
 * @param expandedKeys
 * @param _option
 * @param meta
 */
const handleExpandCollapse = (
  expandedKeys: string[],
  _option: Array<TreeOption | null>,
  meta: { node: TreeOption | null; action: 'expand' | 'collapse' | 'filter' }
) => {
  expandedKeysRef.value = expandedKeys;

  emits('sync-load-node');

  if (meta.node && meta.node.type === 'pod') {
    return;
  }

  switch (meta.action) {
    case 'expand':
      meta.node &&
        (meta.node.prefix = () =>
          h(
            NIcon,
            { size: 14 },
            {
              default: () => h(FolderOpen)
            }
          ));
      break;
    case 'collapse':
      meta.node &&
        (meta.node.prefix = () =>
          h(
            NIcon,
            { size: 14 },
            {
              default: () => h(Folder)
            }
          ));
      break;
  }
};

/**
 * 处理节点行为
 *
 * @param option
 */
const nodeProps = ({ option }: { option: TreeOption }) => {
  return {
    onClick: async () => {
      await nextTick();

      emits('sync-load-node');

      if (option.isLeaf) {
        mittBus.emit('connect-terminal', option);
      }
    },
    onContextmenu(e: MouseEvent): void {
      currentNodeInfo.value = option;

      handleFilter(option);

      showDropdown.value = true;
      dropdownX.value = e.clientX;
      dropdownY.value = e.clientY;
      e.preventDefault();
    }
  };
};

/**
 * 过滤右键菜单行为
 *
 * @param option
 */
const handleFilter = (option: TreeOption) => {
  if (option.isLeaf) {
    dropdownOptions.value= [{
      label: t('Connect'),
      key: 'connect',
      icon: () => h(NIcon, { size: 15 }, { default: () => h(Terminal2) })
    }]
    return;
  }
  if (!option.isLeaf && !option?.isParent) {
    dropdownOptions.value= [{
      label: t('Expand'),
      key: 'expand',
      icon: () => h(NIcon, { size: 15 }, { default: () => h(ExpandCategories) })
    }]
    return;
  }
  dropdownOptions.value = allOptions;
};

/**
 * 右键菜单触发行为
 *
 * @param key
 * @param _option
 */
const handleSelect = (key: string, _option: DropdownOption) => {
  showDropdown.value = false;

  switch (key) {
    case 'expand': {
      if (currentNodeInfo.value) {
        if (
          !expandedKeysRef.value.includes(currentNodeInfo.value.key as string)
        ) {
          expandedKeysRef.value.push(currentNodeInfo.value.key as string);
        }

        handleExpandCollapse(expandedKeysRef.value, [], {
          node: currentNodeInfo.value,
          action: 'expand'
        });

        // 原本的异步加载方法，现在用于自动将宽度展开
        emits('sync-load-node');
      }
      // handleOnLoad(currentNodeInfo.value);
      break;
    }
    case 'connect': {
      mittBus.emit('connect-terminal', currentNodeInfo.value);
      break;
    }
  }
};

/**
 * 根节点连接
 */
const handleRootLink = (e: Event) => {
  e.stopPropagation();
  mittBus.emit('connect-terminal', root.value);
};

const handleClickOutside = () => {
  showDropdown.value = false;
};
</script>

<style scoped lang="scss">
@use './index.scss';
</style>
