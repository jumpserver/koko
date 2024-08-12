<template>
    <n-popover placement="right" trigger="hover">
        <template #trigger>
            <n-button text @click="handleOrganizationIconClick" class="w-full py-[5px]">
                <svg-icon class="organization-icon" :name="name" :icon-style="iconStyle" />
            </n-button>
        </template>
        {{ t('Choosing organization') }}
    </n-popover>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { CSSProperties, h, ref } from 'vue';
import { useDialog, NSelect } from 'naive-ui';

import SvgIcon from '@/components/SvgIcon/index.vue';

defineProps<{
    name: string;
    iconStyle: CSSProperties;
}>();

const { t } = useI18n();
const dialog = useDialog();

const options = [
    { label: 'Option 1', value: 'option1' },
    { label: 'Option 2', value: 'option2' },
    { label: 'Option 3', value: 'option3' }
];

const selectedValue = ref(null);

const handleOrganizationIconClick = () => {
    dialog.success({
        showIcon: false,
        title: t('Choosing organization'),
        content: () =>
            h(NSelect, {
                value: selectedValue.value,
                options: options,
                placeholder: 'Please select an option',
                style: 'width: 100%; margin-top: 20px;',
                'onUpdate:value': value => {
                    selectedValue.value = value;
                }
            }),
        positiveText: 'OK',
        onPositiveClick: () => {
            console.log('Selected value:', selectedValue.value);
        }
    });
};
</script>

<style scoped lang="scss">
.organization-icon:hover {
    fill: #1ab394 !important;
}
</style>
