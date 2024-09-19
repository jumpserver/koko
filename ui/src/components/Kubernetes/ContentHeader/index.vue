<template>
    <n-flex align="center" class="h-[35px] bg-[#3C3C3C]" style="flex-wrap: nowrap">
        <n-flex>
            <Logo class="ml-[10px]" :logo-image="setting.INTERFACE?.logo_logout" />
        </n-flex>
        <!--        <n-flex class="w-[calc(100%-35px)]" justify="center" align="center">-->
        <!--            <n-input-->
        <!--                autosize-->
        <!--                bordered-->
        <!--                clearable-->
        <!--                size="small"-->
        <!--                class="h-[25px] rounded-[5px]"-->
        <!--                style="min-width: 50%"-->
        <!--                placeholder="Please enter the keyword you want to search"-->
        <!--                @update:value="handleSearch"-->
        <!--            >-->
        <!--                <template #prefix>-->
        <!--                    <n-icon :component="Search24Regular" />-->
        <!--                </template>-->
        <!--            </n-input>-->
        <!--            <template v-if="currentKeyWord">-->
        <!--                <n-flex align="center" style="column-gap: 0">-->
        <!--                    <n-icon-->
        <!--                        class="w-[25px] h-[25px] flex justify-center items-center cursor-pointer text-white rounded-[5px] hover:bg-[#101014] transition-colors duration-300"-->
        <!--                        size="16"-->
        <!--                        :component="ArrowSortUp24Regular"-->
        <!--                        @click="findPrev"-->
        <!--                    />-->
        <!--                    <n-icon-->
        <!--                        class="w-[25px] h-[25px] flex justify-center items-center cursor-pointer text-white rounded-[5px] hover:bg-[#101014] transition-colors duration-300"-->
        <!--                        size="16"-->
        <!--                        :component="ArrowSortDown24Regular"-->
        <!--                        @click="findNext"-->
        <!--                    />-->
        <!--                </n-flex>-->
        <!--            </template>-->
        <!--        </n-flex>-->
    </n-flex>
</template>

<script setup lang="ts">
import { storeToRefs } from 'pinia';
import { useParamsStore } from '@/store/modules/params.ts';

// import { ArrowSortDown24Regular, ArrowSortUp24Regular, Search24Regular } from '@vicons/fluent';

import Logo from '@/components/Kubernetes/Sidebar/components/Logo/index.vue';
// import mittBus from '@/utils/mittBus.ts';
// import { ref } from 'vue';

const paramsStore = useParamsStore();
const { setting } = storeToRefs(paramsStore);

// const currentKeyWord = ref('');

// const handleSearch = (value: string) => {
//     currentKeyWord.value = value;
//     mittBus.emit('terminal-search', { keyword: value });
// };
//
// const findNext = () => {
//     mittBus.emit('terminal-search', { keyword: currentKeyWord.value, type: 'next' });
// };
//
// const findPrev = () => {
//     mittBus.emit('terminal-search', { keyword: currentKeyWord.value, type: 'prev' });
// };
</script>
