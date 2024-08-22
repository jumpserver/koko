<template>
    <n-image
        lazy
        :src="logoImage"
        alt="Logo 图片"
        class="h-[35px] w-[35px] justify-center object-fill hover: cursor-pointer"
    />
</template>

<script setup lang="ts">
defineProps<{
    logoImage: string | undefined;
}>();
</script>
