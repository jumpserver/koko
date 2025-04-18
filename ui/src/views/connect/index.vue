<template>
  <div class="h-full w-full">
    <!-- prettier-ignore -->
    <Terminal @update:drawer="handleUpdateDrawer" />

    <!-- prettier-ignore -->
    <Drawer 
      :title="title"
      :show-drawer="showDrawer"
      :content-type="contentType"
      @update:open="showDrawer = $event"
    />
  </div>
</template>

<script setup lang="ts">
import Drawer from '@/components/Drawer/index.vue';
import Terminal from '@/components/Terminal/index.vue';

import { ref, onMounted } from 'vue';

const title = ref('');
const contentType = ref<'setting' | 'file-manager'>('setting');
const showDrawer = ref<boolean>(false);

const handleUpdateDrawer = (show: boolean, _title: string, _contentType: 'setting' | 'file-manager') => {
  title.value = _title;
  showDrawer.value = show;
  contentType.value = _contentType;
};

// const receivePostMessage = (): void => {
//   window.addEventListener('message', (e: MessageEvent) => {
//     const windowMessage = e.data;

//     switch (windowMessage.name) {
//       case WINDOW_MESSAGE_TYPE.PING:
//         lunaId.value = windowMessage.id;
//         origin.value = e.origin;

//         sendEventToLuna(WINDOW_MESSAGE_TYPE.PONG, '', lunaId.value, origin.value);
//         break;
//       case WINDOW_MESSAGE_TYPE.OPEN:
//         title.value = t('Settings');
//         contentType.value = 'setting';

//         showDrawer.value = true;
//         break;
//       case WINDOW_MESSAGE_TYPE.FILE:
//         title.value = t('FileManager');
//         contentType.value = 'file-manager';
//         break;
//     }
//   });
// };

onMounted(() => {
  // receivePostMessage();
});
</script>
