<script setup lang="ts">
import { NTag } from 'naive-ui';
import { useI18n } from 'vue-i18n';

import { useSessionAdapter } from '@/hooks/useSessionAdapter';
import CardContainer from '@/components/CardContainer/index.vue';

import CreateLink from './widget/CreateLink.vue';

const { onlineUsers, shareInfo, removeShareUser } = useSessionAdapter();

const { t } = useI18n();

const handleRemoveShareUser = (userId: string) => {
  const currentDeleteUser = onlineUsers.value.find(user => user.user_id === userId && !user.primary);

  if (!currentDeleteUser) return;

  removeShareUser(currentDeleteUser);
};
</script>

<template>
  <n-flex vertical align="center">
    <CardContainer>
      <template #custom-header>
        <n-text class="text-xs-plus">
          {{ t('OnlineUser') }}
        </n-text>

        <NTag round :bordered="false" type="success" size="small" class="ml-2">
          {{ onlineUsers?.length || 0 }}
        </NTag>
      </template>

      <n-flex v-if="onlineUsers?.length > 0" class="w-full mb-4">
        <UserItem
          v-for="currentUser in onlineUsers"
          :key="currentUser.user_id"
          :username="currentUser.user"
          :primary="currentUser.primary"
          :writable="currentUser.writable"
          :user-id="currentUser.user_id"
          @remove-user="handleRemoveShareUser"
        />
      </n-flex>
    </CardContainer>

    <CardContainer :title="t('ShareLink')">
      <CreateLink :disabled-create-link="!shareInfo.enableShare" />
    </CardContainer>
  </n-flex>
</template>
