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
                                draggable
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
                                :on-load="handleOnLoad"
                                :pattern="searchPattern"
                                :expanded-keys="expandedKeysRef"
                                :allow-checking-not-loaded="true"
                                :on-update:expanded-keys="handleExpandCollapse"
                            />
                            <!-- checkable -->
                        </n-collapse-item>
                    </n-scrollbar>
                </n-collapse>
            </n-descriptions-item>
        </n-descriptions>

        <!-- Context Menu -->
        <n-dropdown
            trigger="manual"
            placement="bottom-start"
            :show="showDropdown"
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
import { ref, h, watch, Ref, nextTick } from 'vue';
import { NIcon, TreeOption, DropdownOption } from 'naive-ui';
import { Folder, FolderOpenOutline, EllipsisHorizontal } from '@vicons/ionicons5';
import { showToolTip } from '../helper/index';
import mittBus from '@/utils/mittBus.ts';

import { useTreeStore } from '@/store/modules/tree.ts';
import { storeToRefs } from 'pinia';

const treeStore = useTreeStore();

const { treeNodes, loadingTreeNode } = storeToRefs(treeStore);

// const props = defineProps<{
//   treeNodes: any;
// }>();

const emits = defineEmits<{
    (e: 'sync-load-node', data: TreeOption): void;
}>();

const { t } = useI18n();
const searchPattern = ref('');
const showDropdown = ref(false);
const dropdownOptions = ref<DropdownOption[]>([]);
const dropdownX = ref(0);
const dropdownY = ref(0);

const expandedKeysRef = ref<string[]>([]);
// const isLoaded = ref(false);
// const treeData: Ref<TreeOption[]> = ref([]);

// watch(
//   () => props.treeNodes,
//   newNode => {
//     isLoaded.value = true;
//     treeData.value = newNode;
//   },
//   {
//     deep: true
//   }
// );

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

const handleOnLoad = (node: TreeOption) => {
    emits('sync-load-node', node);

    return new Promise<boolean>(resolve => {
        if (loadingTreeNode.value) resolve(false);
    });
};

const nodeProps = ({ option }: { option: TreeOption }) => {
    return {
        onClick: async () => {
            await nextTick();

            if (option.isLeaf) {
                mittBus.emit('connect-terminal', option);
            }
        },
        onContextmenu(e: MouseEvent): void {
            dropdownOptions.value = [option];
            showDropdown.value = true;
            dropdownX.value = e.clientX;
            dropdownY.value = e.clientY;
            e.preventDefault();
        }
    };
};

const handleSelect = () => {
    showDropdown.value = false;
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
                            white-space: nowrap; // 添加这一行来防止文字换行
                        }
                    }
                }
            }
        }
    }
}
</style>
