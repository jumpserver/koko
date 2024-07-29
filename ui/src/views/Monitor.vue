<template>
  <Terminal :connectURL="getConnectURL" />
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { useRoute } from 'vue-router';
import { BASE_WS_URL } from '@/config';
import Terminal from '@/components/Terminal/Terminal.vue';

const route = useRoute();

const getConnectURL = computed(() => {
  const params = route.params;
  const requireParams = new URLSearchParams();

  requireParams.append('type', 'monitor');
  requireParams.append('target_id', params.id as string);

  return `${BASE_WS_URL}/koko/ws/terminal/?${requireParams.toString()}`;
});
</script>
