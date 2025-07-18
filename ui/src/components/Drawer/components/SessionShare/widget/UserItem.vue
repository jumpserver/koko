<script setup lang="ts">
import { ref } from 'vue';
import { Trash2, UserRound } from 'lucide-vue-next';

import { useColor } from '@/hooks/useColor';

defineProps<{
  username: string;
}>();

const { lighten } = useColor();

const isHovered = ref(false);

const options = [
  {
    label: '可编辑',
    value: 'editor',
  },
  {
    label: '只读',
    value: 'readonly',
  },
  {
    label: '管理者',
    value: 'admin',
  },
];

const value = ref('editor');
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
      <n-avatar size="medium" :style="{ backgroundColor: '#6366f1', color: 'white' }">
        <UserRound :size="16" />
      </n-avatar>

      <n-flex vertical class="!gap-0">
        <n-flex align="center" class="!gap-0">
          <n-text strong> 前端开发者 </n-text>
          <NTag round :bordered="false" type="success" size="small" class="ml-2"> 所有者 </NTag>
        </n-flex>
        <n-text depth="3" :style="{ fontSize: '12px' }"> 最后在线：3分钟前 </n-text>
      </n-flex>
    </n-flex>

    <n-flex align="center" :wrap="false">
      <n-select v-model:value="value" size="small" :options="options" style="width: 100px" />
      <n-button secondary type="error" size="small">
        <template #icon>
          <Trash2 :size="16" />
        </template>
      </n-button>
    </n-flex>
  </n-flex>

  <n-divider class="!my-0" />
</template>
