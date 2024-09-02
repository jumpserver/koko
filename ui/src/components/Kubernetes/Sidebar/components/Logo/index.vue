<template>
    <n-image
        lazy
        :src="logoImage"
        alt="Logo 图片"
        class="h-[30px] w-[30px] justify-center object-fill hover: cursor-pointer"
    />
</template>

<script setup lang="ts">
defineProps<{
    logoImage: string | undefined;
}>();
</script>
