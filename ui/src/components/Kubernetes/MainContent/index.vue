<template>
  <n-layout :native-scrollbar="false" content-style="height: 100%">
    <tab />
    <Main>
      <template v-slot:terminal>
        <slot name="terminal"></slot>
      </template>
    </Main>
  </n-layout>
</template>

<script setup lang="ts">
import Tab from '../Tab/index.vue';
import Main from '../Main/index.vue';
</script>
