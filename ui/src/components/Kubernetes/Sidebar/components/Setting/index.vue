<template>
    <n-popover placement="right" trigger="hover">
        <template #trigger>
            <n-icon
                :component="Settings"
                size="30"
                color="#646A73"
                class="hover:!text-white transition-all duration-300 cursor-pointer"
                @click="handleSetting"
            />
        </template>
        {{ t('Custom Setting') }}
    </n-popover>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { Settings } from '@vicons/carbon';

const { t } = useI18n();

const props = defineProps<{
    onClick: () => void;
}>();

const handleSetting = () => {
    props.onClick();
};
</script>
