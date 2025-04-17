<template>
  <n-drawer resizable placement="right" :show="showDrawer" :default-width="502" @close="closeDrawer">
    <n-drawer-content closable :title="title" :native-scrollbar="false" :header-style="DRAWER_HEADER_STYLE">
      <template #header>
        <n-flex align="center">
          <span>{{ title }}</span>
        </n-flex>
      </template>

      <Setting v-if="contentType === 'setting'" />
      <!-- <FileManager v-if="contentType === 'file-manager'" /> -->
    </n-drawer-content>
  </n-drawer>
</template>

<script setup lang="ts">
import Setting from './components/Setting/index.vue';

type ContentType = 'setting' | 'file-manager';

const DRAWER_HEADER_STYLE = {
  height: '55px',
  color: '#EBEBEB',
  fontSize: '16px',
  fontWeight: '500',
  fontFamily: 'PingFang SC'
};

defineProps<{
  title: string;

  showDrawer: boolean;

  contentType: ContentType;
}>();

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void;
}>();

const closeDrawer = () => {
  emit('update:open', false);
};
</script>
