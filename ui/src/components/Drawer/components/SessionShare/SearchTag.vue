<script setup lang="ts">
const props = defineProps<{
  label: string;
  handleClose: () => void;
}>();

const handleTagClose = (e: MouseEvent) => {
  e.stopPropagation();
  props.handleClose();
};

const handleTagMousedown = (e: MouseEvent) => {
  e.preventDefault();
};
</script>

<template>
  <n-tag closable type="primary" size="small" @close="handleTagClose" @mousedown="handleTagMousedown">
    {{ label }}
  </n-tag>
</template>
