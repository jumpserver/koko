<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { computed, ref } from 'vue';
import { Trash2, UserRound } from 'lucide-vue-next';

import { useColor } from '@/hooks/useColor';

const props = defineProps<{
  username: string;

  userId: string;

  writable: boolean;

  primary: boolean;
}>();

const emit = defineEmits<{
  (e: 'removeUser', userId: string): void;
}>();

const { t } = useI18n();
const { lighten } = useColor();

const isHovered = ref(false);

const options = [
  {
    label: t('Writable'),
    value: 'editor',
  },
  {
    label: t('ReadOnly'),
    value: 'readonly',
  },
  {
    label: t('Owner'),
    value: 'admin',
  },
];

const selectionValue = computed(() => {
  if (props.primary) {
    return 'admin';
  }

  if (props.writable) {
    return 'editor';
  }

  return 'readonly';
});

const handleRemoveUser = () => {
  emit('removeUser', props.userId);
};
</script>

<template>
  <n-flex
    align="center"
    justify="space-between"
    class="w-full p-2 rounded-md overflow-hidden transition-all duration-300 cursor-pointer"
    :style="{ backgroundColor: isHovered ? lighten(1) : 'transparent' }"
    @mouseenter="isHovered = true"
    @mouseleave="isHovered = false"
  >
    <n-flex align="center">
      <UserRound :size="18" />

      <n-flex vertical class="!gap-0">
        <n-flex align="center" class="!gap-0">
          <n-text strong class="text-xs-plus">
            {{ username }}
          </n-text>
          <NTag round :bordered="false" :type="primary ? 'success' : 'info'" size="small" class="ml-2">
            {{ primary ? t('PrimaryUser') : t('ShareUser') }}
          </NTag>
        </n-flex>
      </n-flex>
    </n-flex>

    <n-flex align="center" :wrap="false">
      <n-select v-model:value="selectionValue" disabled size="small" :options="options" style="width: 100px" />

      <n-popconfirm
        :ok-text="t('Confirm')"
        :cancel-text="t('Cancel')"
        :negative-button-props="{
          type: 'default',
        }"
        :positive-button-props="{
          type: 'error',
        }"
        @positive-click="handleRemoveUser"
      >
        <template #trigger>
          <n-button secondary type="error" size="small" :disabled="primary">
            <template #icon>
              <Trash2 :size="16" />
            </template>
          </n-button>
        </template>
        <span>{{ t('RemoveUser') }}</span>
      </n-popconfirm>
    </n-flex>
  </n-flex>

  <n-divider class="!my-0" />
</template>
