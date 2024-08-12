<template>
    <n-image
        lazy
        :src="logoImage"
        alt="Logo 图片"
        class="h-8 w-8 justify-center object-fill hover: cursor-pointer"
    />
</template>

<script setup lang="ts">
defineProps<{
    logoImage: string;
}>();
</script>
