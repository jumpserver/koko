<template>
  <div class="h-full w-full">
   <FileManager :sftp-token="token" />
  </div>
</template>

<script setup lang="ts">
import FileManager from '@/components/Drawer/components/FileManagement/index.vue';

// 从 url params 中获取 token
const token = new URLSearchParams(window.location.search).get('token') || '';

</script>