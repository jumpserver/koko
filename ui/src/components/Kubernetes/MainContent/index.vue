<template>
  <n-layout :native-scrollbar="false" content-style="height: 100%">
    <tab></tab>
    <Main></Main>
  </n-layout>
</template>

<script setup lang="ts">
import Tab from '../Tab/index.vue';
import Main from '../Main/index.vue';
</script>
