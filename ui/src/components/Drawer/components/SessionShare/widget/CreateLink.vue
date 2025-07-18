<script setup lang="ts">
import { reactive } from 'vue';
import { useI18n } from 'vue-i18n';
import { Search } from 'lucide-vue-next';

import { getMinuteLabel } from '@/utils';

const { t } = useI18n();

const expiredOptions = reactive([
  { label: getMinuteLabel(1, t), value: 1 },
  { label: getMinuteLabel(5, t), value: 5 },
  { label: getMinuteLabel(10, t), value: 10 },
  { label: getMinuteLabel(20, t), value: 20 },
  { label: getMinuteLabel(60, t), value: 60 },
]);

const actionsPermOptions = reactive([
  { label: t('Writable'), value: 'writable' },
  { label: t('ReadOnly'), value: 'readonly' },
]);
</script>

<template>
  <n-descriptions label-placement="top" :column="1">
    <n-descriptions-item>
      <template #label>
        <n-text strong> 会话持续时间 </n-text>

        <n-tag round :bordered="false" type="info" size="small"> 必选 </n-tag>
      </template>

      <n-flex align="center" class="mt-2 cursor-pointer">
        <n-card
          v-for="item in expiredOptions"
          :key="item.value"
          bordered
          hoverable
          size="small"
          :content-style="{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }"
          style="width: 100px; height: 45px"
        >
          <n-text strong depth="1">
            {{ item.label }}
          </n-text>
        </n-card>
      </n-flex>
    </n-descriptions-item>

    <n-descriptions-item>
      <n-divider dashed class="!my-1" />
    </n-descriptions-item>

    <n-descriptions-item>
      <template #label>
        <n-text strong> 权限设置 </n-text>

        <n-tag round :bordered="false" type="info" size="small"> 必选 </n-tag>
      </template>

      <n-flex align="center" :wrap="false" class="mt-2 cursor-pointer">
        <n-card
          v-for="item in actionsPermOptions"
          :key="item.value"
          bordered
          hoverable
          size="small"
          :content-style="{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }"
          style="width: 50%"
        >
          <n-text strong depth="1">
            {{ item.label }}
          </n-text>
        </n-card>
      </n-flex>
    </n-descriptions-item>
    <n-descriptions-item>
      <n-divider dashed class="!my-1" />
    </n-descriptions-item>

    <n-descriptions-item>
      <template #label>
        <n-text strong> 参与者 </n-text>

        <n-tag round :bordered="false" type="info" size="small"> 可选 </n-tag>
      </template>

      <n-flex vertical class="!my-1">
        <n-input placeholder="搜索成员">
          <template #prefix>
            <Search :size="16" />
          </template>
        </n-input>
      </n-flex>
    </n-descriptions-item>

    <n-descriptions-item>
      <n-divider class="!my-1" />
    </n-descriptions-item>

    <n-descriptions-item>
      <n-button block secondary type="primary"> 创建链接 </n-button>
    </n-descriptions-item>
  </n-descriptions>
</template>
