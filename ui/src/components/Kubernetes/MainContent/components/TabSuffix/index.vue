<template>
    <n-flex justify="space-between" align="center" class="mr-[15px]" style="column-gap: 5px">
        <n-popover
            v-for="(option, index) in suffixOptions"
            :key="index"
            :trigger="option.iconName === 'search' ? 'click' : 'hover'"
        >
            <template #trigger>
                <div class="flex items-center cursor-pointer">
                    <n-icon :component="option.component" @click="option.onClick" />
                </div>
            </template>

            <template #header v-if="option.iconName === 'search'">
                <n-text strong depth="1">
                    <div>{{ option.label }}</div>
                </n-text>
                <n-input
                    clearable
                    size="small"
                    bordered
                    show-count
                    :maxlength="12"
                    class="mt-[20px]"
                    @update:value="handleSearch"
                >
                    <template #prefix>
                        <n-icon :component="Search" />
                    </template>
                    <template #suffix>
                        <n-icon
                            class="cursor-pointer rounded-[5px] hover:bg-[#363737] hover:text-white"
                            size="16"
                            :component="ArrowSortUp24Regular"
                        />
                        <n-icon
                            class="cursor-pointer rounded-[5px] hover:bg-[#363737] hover:text-white"
                            size="16"
                            :component="ArrowSortDown24Regular"
                        />
                    </template>
                </n-input>
            </template>

            <template #default v-else>
                <div>
                    {{ option.label }}
                </div>
            </template>
        </n-popover>
    </n-flex>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { useDebounceFn } from '@vueuse/core';
import { SplitVertical20Regular, ArrowSortDown24Regular, ArrowSortUp24Regular } from '@vicons/fluent';
import { EllipsisHorizontal, Search } from '@vicons/ionicons5';

import { Component, ref } from 'vue';
import mittBus from '@/utils/mittBus.ts';

interface ISuffixOptions {
    iconName: string;

    label: string;

    component: Component;

    onClick: () => void;
}

const { t } = useI18n();

const showInput = ref(false);

const suffixOptions: ISuffixOptions[] = [
    {
        iconName: 'search',
        label: t('Search'),
        component: Search,
        onClick: () => {
            showInput.value = true;
        }
    },
    {
        iconName: 'split',
        label: t('Split'),
        component: SplitVertical20Regular,
        onClick: () => {}
    }
];

const handleSearch = (value: string) => {
    mittBus.emit('terminal-search', { keyword: value });
};
</script>

<style scoped lang="scss"></style>
