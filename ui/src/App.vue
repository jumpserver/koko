<script setup lang="ts">
import { zhCN, dateZhCN } from 'naive-ui';
</script>

<template>
	<n-config-provider :locale="zhCN" :date-locale="dateZhCN">
		<n-message-provider>
			<router-view />
		</n-message-provider>
	</n-config-provider>
</template>

<style scoped></style>
