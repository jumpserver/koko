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
                    <n-scrollbar style="max-height: calc(100vh - 30px)">
                        <n-collapse-item title="Kubernetes" class="collapse-item" name="asset-tree">
                            <n-tree
                                cascade
                                show-line
                                block-node
                                block-line
                                expand-on-click
                                class="tree-item"
                                check-strategy="all"
                                checkbox-placement="left"
                                :render-label="showToolTip"
                                :data="treeNodes"
                                :node-props="nodeProps"
                                :on-load="useDebounceFn(handleOnLoad, 300)"
                                :pattern="searchPattern"
                                :expanded-keys="expandedKeysRef"
                                :allow-checking-not-loaded="true"
                                :on-update:expanded-keys="handleExpandCollapse"
                            />
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
            @clickoutside="handleClickoutside"
        />
    </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { storeToRefs } from 'pinia';
import { ref, h, nextTick } from 'vue';
import { useDebounceFn } from '@vueuse/core';
import { showToolTip } from '../helper/index';
import { useTreeStore } from '@/store/modules/tree.ts';

import mittBus from '@/utils/mittBus.ts';

import { NIcon, TreeOption, DropdownOption } from 'naive-ui';
import { Folder, FolderOpenOutline, EllipsisHorizontal, ExpandSharp, LinkSharp } from '@vicons/ionicons5';

const { t } = useI18n();
const treeStore = useTreeStore();

const { treeNodes } = storeToRefs(treeStore);

const emits = defineEmits<{
    (e: 'sync-load-node', data: TreeOption): void;
}>();

const dropdownY = ref(0);
const dropdownX = ref(0);
const searchPattern = ref('');
const showDropdown = ref(false);
const currentNodeInfo = ref();
const expandedKeysRef = ref<string[]>([]);
const dropdownOptions = ref<DropdownOption[]>([]);

const allOptions = [
    {
        label: '展开',
        key: 'expand',
        icon: () => h(NIcon, null, { default: () => h(ExpandSharp) })
    },
    {
        label: '连接',
        key: 'connect',
        icon: () => h(NIcon, null, { default: () => h(LinkSharp) })
    }
];

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

/**
 * @description 处理节点行为
 * @param option
 */
const nodeProps = ({ option }: { option: TreeOption }) => {
    return {
        onClick: async () => {
            await nextTick();

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

const handleFilter = (option: TreeOption) => {
    dropdownOptions.value = allOptions.filter(item => {
        if (option.isLeaf) {
            return item.key === 'connect';
        } else if (!option.isLeaf && !option?.isParent) {
            return item.key === 'expand';
        } else {
            return true;
        }
    });
};

const handleOnLoad = (node: TreeOption) => {
    treeStore.setCurrentNode(node);

    emits('sync-load-node', node);

    if (!expandedKeysRef.value.includes(node.key as string)) {
        setTimeout(() => {
            expandedKeysRef.value.push(node.key as string);
        }, 300);
    }

    return new Promise<boolean>(resolve => {
        resolve(false);
    });
};

const handleSelect = (key: string, _option: DropdownOption) => {
    showDropdown.value = false;

    switch (key) {
        case 'expand': {
            handleOnLoad(currentNodeInfo.value);
            break;
        }
        case 'connect': {
            mittBus.emit('connect-terminal', currentNodeInfo.value);
            break;
        }
    }
};

const handleClickoutside = () => {
    showDropdown.value = false;
};
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
        height: calc(100vh - 35px - 35px);
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
            margin-top: 5px;
            margin-left: 15px;

            .n-collapse-item__content-inner {
                padding-top: 0;

                .tree-item .n-tree-node-wrapper {
                    padding: 0 0 5px 0;
                    line-height: 22px;

                    .n-tree-node-content {
                        padding-left: 5px;

                        .n-tree-node-content__text {
                            white-space: nowrap;
                        }
                    }
                }
            }
        }
    }
}
</style>
