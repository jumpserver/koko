<template>
  <div class="h-full w-full">
    <!-- prettier-ignore -->
    <Terminal :content-type="contentType" @update:drawer="handleUpdateDrawer" @update:protocol="handleUpdateProtocol" />

    <!-- prettier-ignore -->
    <Drawer
      :title="title"
      :token="token"
      :show-drawer="showDrawer"
      :content-type="contentType"
      :default-protocol="defaultProtocol"
      @update:open="showDrawer = $event"
      @update:content-type="contentType = $event"
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
const defaultProtocol = ref<string>('ssh');
const contentType = ref<ContentType>('');
const showDrawer = ref<boolean>(false);

const handleUpdateDrawer = (show: boolean, _title: string, _contentType: ContentType, _token?: string) => {
  showDrawer.value = show;

  title.value = _title;
  token.value = _token || '';
  contentType.value = _contentType;
};

const handleUpdateProtocol = (protocol: string) => {
  defaultProtocol.value = protocol;
};
</script>
