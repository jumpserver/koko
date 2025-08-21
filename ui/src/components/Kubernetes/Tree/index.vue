<script setup lang="ts">
import type { DropdownOption, TreeOption } from 'naive-ui';

import { useI18n } from 'vue-i18n';
import { storeToRefs } from 'pinia';
import { NPopover } from 'naive-ui';
import { computed, h, nextTick, onMounted, onUnmounted, ref, watchEffect } from 'vue';
import { Folder, FolderOpen, RefreshCcw, Search, SquareTerminal, UnfoldVertical } from 'lucide-vue-next';

import type { customTreeOption } from '@/types/modules/config.type';

import mittBus from '@/utils/mittBus';
import { useColor } from '@/hooks/useColor';
import { useTreeStore } from '@/store/modules/tree.ts';

const emits = defineEmits<{
  (e: 'syncLoadNode', data?: TreeOption): void;
  (e: 'reloadTree'): void;
}>();
const { t } = useI18n();
const { lighten, darken } = useColor();
const treeStore = useTreeStore();

const { treeNodes, root } = storeToRefs(treeStore);

const themeColors = computed(() => {
  const colors = {
    '--tree-header-text-color': lighten(55),
    '--tree-node-text-color': lighten(60),
    '--tree-bg-color': darken(8),
    '--tree-hover-color': lighten(8),
  };

  return colors;
});

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
    icon: () => h(UnfoldVertical, { size: 15 }),
  },
  {
    label: t('Connect'),
    key: 'connect',
    icon: () => h(SquareTerminal, { size: 15 }),
  },
];
const buttonGroups = [
  {
    label: t('Connect'),
    icon: () => h(SquareTerminal, { size: 15 }),
    click: (e: Event) => {
      handleRootLink(e);
    },
  },
  {
    label: t('Search'),
    icon: Search,
    click: (e: Event) => {
      e.stopPropagation();
      showSearch.value = !showSearch.value;
    },
  },
  {
    label: t('Refresh'),
    icon: RefreshCcw,
    click: (e: Event) => {
      e.stopPropagation();
      isLoaded.value = false;
      emits('reloadTree');
    },
  },
];

watchEffect(() => {
  if (treeNodes.value.length > 0) {
    isLoaded.value = true;
  }
});

const showToolTip = (option: TreeOption) => {
  const customOption = option.option as customTreeOption;

  return h(
    NPopover,
    {
      trigger: 'hover',
      placement: 'top-start',
      delay: 1000,
    },
    {
      trigger: () => h('span', { style: { display: 'inline-block', whiteSpace: 'nowrap' } }, customOption.label),
      default: () => customOption.label,
    }
  );
};

/**
 * @description 处理节点展开
 */
function handleExpandCollapse(
  expandedKeys: string[],
  _option: Array<TreeOption | null>,
  meta: { node: TreeOption | null; action: 'expand' | 'collapse' | 'filter' }
) {
  expandedKeysRef.value = expandedKeys;

  emits('syncLoadNode');

  if (meta.node && meta.node.type === 'pod') {
    return;
  }

  switch (meta.action) {
    case 'expand':
      meta.node && (meta.node.prefix = () => h(FolderOpen, { size: 14 }));
      break;
    case 'collapse':
      meta.node && (meta.node.prefix = () => h(Folder, { size: 14 }));
      break;
  }
}

/**
 * 处理节点行为
 */
function nodeProps({ option }: { option: TreeOption }) {
  return {
    onClick: async () => {
      await nextTick();

      emits('syncLoadNode');

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
    },
  };
}

/**
 * 过滤右键菜单行为
 *
 * @param option
 */
function handleFilter(option: TreeOption) {
  if (option.isLeaf) {
    dropdownOptions.value = [
      {
        label: t('Connect'),
        key: 'connect',
        icon: () => h(SquareTerminal, { size: 15 }),
      },
    ];
    return;
  }
  if (!option.isLeaf && !option?.isParent) {
    dropdownOptions.value = [
      {
        label: t('Expand'),
        key: 'expand',
        icon: () => h(UnfoldVertical, { size: 15 }),
      },
    ];
    return;
  }
  dropdownOptions.value = allOptions;
}

/**
 * 右键菜单触发行为
 *
 * @param key
 * @param _option
 */
function handleSelect(key: string, _option: DropdownOption) {
  showDropdown.value = false;

  switch (key) {
    case 'expand': {
      if (currentNodeInfo.value) {
        if (!expandedKeysRef.value.includes(currentNodeInfo.value.key as string)) {
          expandedKeysRef.value.push(currentNodeInfo.value.key as string);
        }

        handleExpandCollapse(expandedKeysRef.value, [], {
          node: currentNodeInfo.value,
          action: 'expand',
        });

        // 原本的异步加载方法，现在用于自动将宽度展开
        emits('syncLoadNode');
      }
      // handleOnLoad(currentNodeInfo.value);
      break;
    }
    case 'connect': {
      mittBus.emit('connect-terminal', currentNodeInfo.value);
      break;
    }
  }
}

/**
 * 根节点连接
 */
function handleRootLink(e: Event) {
  e.stopPropagation();
  mittBus.emit('connect-terminal', root.value);
}

function handleClickOutside() {
  showDropdown.value = false;
}

onMounted(() => {
  mittBus.on('connect-error', () => {
    isLoaded.value = true;
  });
});

onUnmounted(() => {
  mittBus.off('connect-error');
});
</script>

<template>
  <div class="group" :style="themeColors">
    <n-descriptions label-placement="top" class="tree-wrapper">
      <template #header>
        <n-flex align="center" justify="space-between" :style="{ backgroundColor: darken(4) }" class="pl-[24px] h-full">
          <n-text depth="1" class="text-xs">
            {{ t('KubernetesManagement') }}
          </n-text>
        </n-flex>
      </template>
      <n-descriptions-item class="h-full">
        <n-collapse arrow-placement="left" :accordion="true" :default-expanded-names="['asset-tree']">
          <n-scrollbar style="max-height: calc(100vh - 100px)">
            <n-collapse-item :title="root?.label" class="collapse-item" name="asset-tree">
              <template #header-extra>
                <n-flex justify="center" class="mr-[10px] !hidden group-hover:!flex">
                  <template v-for="option of buttonGroups" :key="option.label">
                    <NPopover>
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
                    </NPopover>
                  </template>
                </n-flex>
              </template>
              <n-input
                v-if="showSearch"
                v-model:value="searchPattern"
                clearable
                size="small"
                :placeholder="t('Search')"
                class="mb-[3px] pl-[4px]"
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

<style scoped lang="scss">
@use './index.scss';
</style>
