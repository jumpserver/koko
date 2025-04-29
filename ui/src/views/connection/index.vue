<template>
  <div class="h-full w-full">
    <!-- prettier-ignore -->
    <Terminal @update:drawer="handleUpdateDrawer" />

    <!-- prettier-ignore -->
    <Drawer
      v-if="showDrawer"
      :title="title"
      :token="token"
      :show-drawer="showDrawer"
      :content-type="contentType"
      @update:open="showDrawer = $event"
    />
  </div>
</template>

<script setup lang="ts">
import Drawer from '@/components/Drawer/index.vue';
import Terminal from '@/components/Terminal/index.vue';

import { ref } from 'vue';
import type { ContentType } from '@/types/modules/connection.type';

const title = ref<string>('');
const token = ref<string>('');
const contentType = ref<ContentType>('setting');
const showDrawer = ref<boolean>(false);

const handleUpdateDrawer = (show: boolean, _title: string, _contentType: ContentType, _token?: string) => {
  showDrawer.value = show;

  title.value = _title;
  token.value = _token || '';
  contentType.value = _contentType;
};
</script>
