<template>
    <n-layout-header class="header-tab relative">
        <n-flex ref="el" style="gap: 0">
            <n-tabs
                v-model:value="nameRef"
                size="small"
                type="card"
                closable
                tab-style="min-width: 80px;"
                @close="handleClose"
            >
                <n-tab-pane
                    v-for="panel in panelsRef"
                    :key="panel"
                    :tab="panel.toString()"
                    :name="panel"
                    class="h-screen bg-[#101014]"
                >
                    {{ panel }}
                </n-tab-pane>
                <template v-slot:suffix>
                    <n-flex
                        justify="space-between"
                        align="center"
                        class="h-[35px] mr-[15px]"
                        style="column-gap: 5px"
                    >
                        <n-popover>
                            <template #trigger>
                                <div
                                    class="icon-item flex justify-center items-center w-[25px] h-[25px] cursor-pointer transition-all duration-300 ease-in-out hover:rounded-[5px]"
                                >
                                    <svg-icon name="split" :icon-style="iconStyle" />
                                </div>
                            </template>
                            拆分
                        </n-popover>

                        <n-popover>
                            <template #trigger>
                                <div
                                    class="icon-item flex justify-center items-center w-[25px] h-[25px] cursor-pointer transition-all duration-300 ease-in-out hover:rounded-[5px]"
                                >
                                    <n-icon size="16px" :component="EllipsisHorizontal" />
                                </div>
                            </template>
                            操作
                        </n-popover>
                    </n-flex>
                </template>
            </n-tabs>
        </n-flex>
    </n-layout-header>
</template>

<script setup lang="ts">
import SvgIcon from '@/components/SvgIcon/index.vue';

import { type CSSProperties, ref } from 'vue';
import { EllipsisHorizontal } from '@vicons/ionicons5';
import { useMessage } from 'naive-ui';

const el = ref();

const iconStyle: CSSProperties = {
    width: '16px',
    height: '16px',
    transition: 'fill 0.3s'
};

const nameRef = ref(1);
const message = useMessage();
const panelsRef = ref([1, 2, 3, 4, 5, 6, 7]);
function handleClose(name: number) {
    const { value: panels } = panelsRef;
    if (panels.length === 1) {
        message.error('最后一个了');
        return;
    }
    message.info(`关掉 ${name}`);
    const index = panels.findIndex(v => name === v);
    panels.splice(index, 1);
    if (nameRef.value === name) {
        nameRef.value = panels[index];
    }
}
</script>

<style scoped lang="scss">
$--el-main-tab-bg-color: #252526;
$--el-main-tab-text-color: #ffffff;
$--el-main-tab-icon-color: #c5c5c5;
$--el-main-tab-icon-hover-color: #5a5d5e4f;
$--el-main-text-color: #cccccc;
$--el-main-bg-color: #1e1e1e;

.header-tab {
    width: 100% !important;
    background-color: $--el-main-tab-bg-color;

    .tab-item {
        :deep(.text-item) {
            color: $--el-main-tab-text-color;
        }

        :deep(.close-icon) {
            color: $--el-main-tab-text-color;
        }

        // todo)) 有些问题
        &.first-click {
            font-style: italic;
            color: $--el-main-text-color;
        }

        &.second-click {
            font-style: normal;
            color: rgb(255 255 255 / 50%);
        }

        &.active-tab {
            color: $--el-main-text-color !important;
            background-color: $--el-main-bg-color;
        }
    }

    :deep(.icon-item) {
        svg {
            color: $--el-main-tab-icon-color;
            fill: $--el-main-tab-icon-color;
        }

        &:hover {
            background-color: $--el-main-tab-icon-hover-color;
        }
    }
}
</style>
