<script setup lang="ts">
import type { SelectRenderTag } from 'naive-ui';

import { useI18n } from 'vue-i18n';
import { storeToRefs } from 'pinia';
import { useDebounceFn } from '@vueuse/core';
import { writeText } from 'clipboard-polyfill';
import { Delete, Undo2 } from 'lucide-vue-next';
import { computed, h, reactive, ref, watch } from 'vue';
import { NTag, useDialogReactiveList, useMessage } from 'naive-ui';

import type { ShareUserOptions } from '@/types/modules/user.type';

import mittBus from '@/utils/mittBus';
import { BASE_URL } from '@/utils/config';
import { formatMessage, getMinuteLabel } from '@/utils';
import { useParamsStore } from '@/store/modules/params.ts';
import { useConnectionStore } from '@/store/modules/useConnection.ts';
import { FORMATTER_MESSAGE_TYPE } from '@/types/modules/message.type';

const message = useMessage();
const paramsStore = useParamsStore();
const connectionStore = useConnectionStore();
const dialogReactiveList = useDialogReactiveList();

const { t } = useI18n();

const loading = ref<boolean>(false);
const searchLoading = ref<boolean>(false);
const showModal = ref<boolean>(false);
const showCreateForm = ref<boolean>(true);

const expiredOptions = reactive([
  { label: getMinuteLabel(1, t), value: 1 },
  { label: getMinuteLabel(5, t), value: 5 },
  { label: getMinuteLabel(10, t), value: 10 },
  { label: getMinuteLabel(20, t), value: 20 },
  { label: getMinuteLabel(60, t), value: 60 },
]);

const shareLinkRequest = reactive({
  expiredTime: 10,
  actionPerm: 'writable',
  users: [] as ShareUserOptions[],
});

const actionsPermOptions = reactive([
  { label: t('Writable'), value: 'writable' },
  { label: t('ReadOnly'), value: 'readonly' },
]);

watch(
  () => connectionStore.shareCode,
  nv => {
    if (nv) {
      showCreateForm.value = false;
    }
  }
);

const cardTitle = computed(() => {
  return showCreateForm.value ? t('CreateLink') : t('ShareLink');
});
const shareURL = computed(() => {
  return connectionStore.shareId ? `${BASE_URL}/luna/share/${connectionStore.shareId}/` : t('NoLink');
});
const mappedUserOptions = computed(() => {
  if (connectionStore.userOptions && connectionStore.userOptions.length > 0) {
    return connectionStore.userOptions.map((item: ShareUserOptions) => ({
      label: item.username,
      value: item.id,
    }));
  } else {
    return [];
  }
});

const resetModalState = () => {
  loading.value = false;
  searchLoading.value = false;
  showCreateForm.value = true;
  shareLinkRequest.expiredTime = 10;
  shareLinkRequest.actionPerm = 'writable';
  shareLinkRequest.users = [];
  connectionStore.updateConnectionState({
    shareId: '',
    shareCode: '',
  });
};

const handleBack = () => {
  loading.value = false;
  showCreateForm.value = true;
  connectionStore.updateConnectionState({
    shareId: '',
    shareCode: '',
  });
  shareLinkRequest.expiredTime = 10;
  shareLinkRequest.actionPerm = 'writable';
  shareLinkRequest.users = [];
};

const openModal = () => {
  showModal.value = true;
  showCreateForm.value = true;
};

const handleModalClose = (show: boolean) => {
  if (!show) {
    resetModalState();
  }
};

const copyShareURL = () => {
  if (!connectionStore.shareId) return;
  if (!connectionStore.enableShare) return;

  const url = shareURL.value;
  const linkTitle = t('LinkAddr');
  const codeTitle = t('VerifyCode');

  const text = `${linkTitle}: ${url}\n${codeTitle}: ${connectionStore.shareCode}`;

  writeText(text)
    .then(() => {
      message.success(t('CopyShareURLSuccess'));
    })
    .catch(e => {
      message.error(`Copy Error for ${e}`);
    });

  dialogReactiveList.value.forEach(item => {
    if (item.class === 'share') {
      connectionStore.updateConnectionState({
        shareId: '',
        shareCode: '',
      });
      paramsStore.setShareId('');
      paramsStore.setShareCode('');
      item.destroy();
    }
  });
};

const handleShareURlCreated = () => {
  const { socket, terminalId, sessionId } = storeToRefs(connectionStore);

  if (!socket?.value || !terminalId?.value || !sessionId?.value) {
    return message.error(t('创建连接失败'));
  }

  loading.value = true;

  socket.value.send(
    formatMessage(
      terminalId.value,
      FORMATTER_MESSAGE_TYPE.TERMINAL_SHARE,
      JSON.stringify({
        origin: window.location.origin,
        session: sessionId.value,
        users: shareLinkRequest.users,
        expired_time: shareLinkRequest.expiredTime,
        action_permission: shareLinkRequest.actionPerm,
      })
    )
  );
};

const handleSearch = (_query: string) => {
  searchLoading.value = true;

  const { socket, terminalId } = storeToRefs(connectionStore);

  if (!socket?.value || !terminalId?.value) {
    return;
  }

  socket.value.send(
    formatMessage(
      terminalId.value,
      FORMATTER_MESSAGE_TYPE.TERMINAL_GET_SHARE_USER,
      JSON.stringify({
        query: _query,
      })
    )
  );

  setTimeout(() => {
    searchLoading.value = false;
  }, 500);
};

const handleRemoveShareUser = (user: ShareUserOptions) => {
  if (!connectionStore.sessionId) {
    return;
  }

  mittBus.emit('remove-share-user', {
    sessionId: connectionStore.sessionId,
    userMeta: user,
    type: 'remove',
  });
};

const renderTag: SelectRenderTag = ({ option, handleClose }) => {
  return h(
    NTag,
    {
      closable: true,
      size: 'small',
      type: 'primary',
      onMousedown: (e: FocusEvent) => {
        e.preventDefault();
      },
      onClose: (e: MouseEvent) => {
        e.stopPropagation();
        handleClose();
      },
    },
    {
      default: () => option.label,
    }
  );
};

const debounceSearch = useDebounceFn(handleSearch, 300);
</script>

<template>
  <n-flex vertical align="center">
    <n-divider dashed title-placement="left" class="!mb-3 !mt-0">
      <n-text depth="2" class="text-sm opacity-70"> 在线用户（{{ connectionStore.onlineUsers?.length || 0 }}） </n-text>
    </n-divider>

    <n-flex v-if="connectionStore.onlineUsers?.length" class="w-full mb-4">
      <n-list class="w-full" bordered hoverable>
        <n-list-item v-for="user in connectionStore.onlineUsers" :key="user.user_id">
          <template #suffix>
            <Delete
              v-if="!user.primary"
              :size="18"
              class="cursor-pointer hover:text-red-500 transition-all duration-200"
              @click="handleRemoveShareUser(user)"
            />
          </template>

          <n-flex vertical>
            <n-text>{{ user.user }}</n-text>
            <n-flex :size="8">
              <NTag :bordered="false" size="small" :type="user.primary ? 'info' : 'default'">
                {{ user.primary ? '主用户' : '共享用户' }}
              </NTag>
              <NTag :bordered="false" :type="user.writable ? 'warning' : 'success'" size="small">
                {{ user.writable ? t('Writable') : t('ReadOnly') }}
              </NTag>
            </n-flex>
          </n-flex>
        </n-list-item>
      </n-list>

      <n-button
        type="primary"
        size="small"
        class="!w-full mt-1"
        :disabled="!connectionStore.enableShare"
        @click="openModal"
      >
        <n-text class="text-white text-sm">
          {{ t('CreateLink') }}
        </n-text>
      </n-button>
    </n-flex>

    <n-modal v-model:show="showModal" :auto-focus="false" @update:show="handleModalClose">
      <n-card style="width: 600px" bordered :title="cardTitle" role="dialog" size="large">
        <Transition name="fade" mode="out-in">
          <div v-if="showCreateForm" key="create-form" class="min-h-[305px] w-full">
            <n-form label-placement="top" :model="shareLinkRequest">
              <n-grid :cols="24">
                <n-form-item-gi :span="24" :label="t('ExpiredTime')">
                  <n-select
                    v-model:value="shareLinkRequest.expiredTime"
                    size="small"
                    :placeholder="t('SelectAction')"
                    :options="expiredOptions"
                  />
                </n-form-item-gi>
                <n-form-item-gi :span="24" :label="t('ActionPerm')">
                  <n-select
                    v-model:value="shareLinkRequest.actionPerm"
                    size="small"
                    :placeholder="t('ActionPerm')"
                    :options="actionsPermOptions"
                  />
                </n-form-item-gi>
                <n-form-item-gi :span="24" :label="t('ShareUser')">
                  <n-select
                    v-model:value="shareLinkRequest.users"
                    multiple
                    filterable
                    clearable
                    remote
                    size="small"
                    :loading="searchLoading"
                    :render-tag="renderTag"
                    :options="mappedUserOptions"
                    :clear-filter-after-select="false"
                    :placeholder="t('GetShareUser')"
                    @search="debounceSearch"
                  />
                </n-form-item-gi>
              </n-grid>

              <n-button
                type="primary"
                size="small"
                class="!w-full mt-1"
                :loading="loading"
                :disabled="!connectionStore.enableShare"
                @click="handleShareURlCreated"
              >
                <n-text class="text-white text-sm">
                  {{ t('CreateLink') }}
                </n-text>
              </n-button>
            </n-form>
          </div>

          <div v-else key="share-result" class="relative min-h-[305px] w-full">
            <n-result status="success" :description="t('CreateSuccess')" class="relative" />

            <n-tooltip size="small">
              <template #trigger>
                <Undo2
                  :size="16"
                  class="absolute top-0 right-0 focus:outline-none cursor-pointer"
                  @click="handleBack"
                />
              </template>
              <span>{{ t('Back') }}</span>
            </n-tooltip>

            <n-form label-placement="top">
              <n-grid :cols="24">
                <n-form-item-gi :label="t('LinkAddr')" :span="24">
                  <n-input readonly :value="shareURL" />
                </n-form-item-gi>
                <n-form-item-gi :label="t('VerifyCode')" :span="24">
                  <n-input readonly :loading="!connectionStore.shareCode" :value="connectionStore.shareCode" />
                </n-form-item-gi>
              </n-grid>

              <n-button type="primary" size="small" class="!w-full" @click="copyShareURL">
                <n-text class="text-white text-sm">
                  {{ t('CopyLink') }}
                </n-text>
              </n-button>
            </n-form>
          </div>
        </Transition>
      </n-card>
    </n-modal>
  </n-flex>
</template>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition:
    opacity 0.5s ease,
    transform 0.5s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(10px);
}
</style>
