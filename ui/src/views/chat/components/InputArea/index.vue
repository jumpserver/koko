<template>
  <n-flex vertical class="w-full h-full flex-1 px-4 py-4 border-t-1 border-[#1a1a1a]">
    <n-flex align="center" justify="space-between" class="w-full h-8 !gap-0">
      <n-space class="w-1/2">
        <n-select v-model:value="roleType" size="small" :options="roleTypeOptions" class="!w-42" />
      </n-space>

      <n-space justify="end" class="w-1/2">
        <Eraser :size="20" class="icon-hover-primary" />
      </n-space>
    </n-flex>

    <n-divider dashed class="!my-1" />

    <textarea v-model="inputValue" placeholder="询问任何问题" class="w-full h-full resize-none outline-none" />

    <n-flex justify="end" align="center">
      <n-text depth="1"> </n-text>
      <n-button type="primary" class="!w-18 !h-8" @click="handleSendMessage"> 发送 </n-button>
    </n-flex>
  </n-flex>
</template>

<script setup lang="ts">
import { BASE_URL } from '@/config';
import { ref, onMounted } from 'vue';
import { alovaInstance } from '@/api';
import { Eraser } from 'lucide-vue-next';

import type { SelectOption } from 'naive-ui';
import type { ChatSendMessage, RoleType } from '@/types/modules/chat.type';

const emits = defineEmits<{
  (e: 'send-message', message: ChatSendMessage): void;
}>();

const roleType = ref('');
const inputValue = ref('');
const roleTypeList = ref<RoleType[]>([]);
const roleTypeOptions = ref<SelectOption[]>([]);

const handleSendMessage = () => {
  emits('send-message', {
    data: inputValue.value,
    id: '',
    prompt: ''
  });
};

onMounted(async () => {
  roleTypeList.value = await alovaInstance
    .Get(`${BASE_URL}/api/v1/settings/chatai-prompts/`)
    .then(response => (response as Response).json());

  roleTypeOptions.value = roleTypeList.value.map(item => ({
    label: item.name,
    value: item.name
  }));

  console.log(roleTypeList.value);
});
</script>
