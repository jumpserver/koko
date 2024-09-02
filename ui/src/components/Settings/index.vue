<template>
    <div>
        <n-drawer v-model:show="showDrawer" :width="260">
            <n-drawer-content :native-scrollbar="false" :title="t('Settings')" closable>
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
                                                {{ `(${item.content.length})` }}
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
                                                        size="small"
                                                        class="mt-[2.5px] mb-[2.5px] mx-[25px] w-[170px] justify-around cursor-pointer"
                                                        :bordered="false"
                                                        :type="
                                                            item.content.indexOf(detail) !== 0
                                                                ? 'info'
                                                                : 'success'
                                                        "
                                                        :closable="true"
                                                        :disabled="item.content.indexOf(detail) === 0"
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
                                                    <span>{{ detail.tip }}</span>
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
                                                            <n-icon
                                                                size="16"
                                                                class="ml-[5px] mr-[5px]"
                                                                :component="detail.icon"
                                                            />
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
            </n-drawer-content>
        </n-drawer>
    </div>
</template>

<script setup lang="ts">
import mittBus from '@/utils/mittBus.ts';

import { onMounted, onUnmounted, ref } from 'vue';
import { ISettingProp } from '@/views/interface';
import { useI18n } from 'vue-i18n';

withDefaults(
    defineProps<{
        settings: ISettingProp[];
    }>(),
    {}
);

const { t } = useI18n();

const showDrawer = ref<boolean>(false);

onMounted(() => {
    mittBus.on('open-setting', () => {
        showDrawer.value = !showDrawer.value;
    });
});

onUnmounted(() => {
    mittBus.off('open-setting');
});
</script>
