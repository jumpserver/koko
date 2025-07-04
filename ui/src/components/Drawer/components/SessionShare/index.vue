<script setup lang="ts">
import type { SelectRenderTag } from 'naive-ui';

import { useI18n } from 'vue-i18n';
import { NTag, useMessage } from 'naive-ui';
import { useDebounceFn } from '@vueuse/core';
import { computed, h, reactive, ref, watch } from 'vue';
import { Crown, Delete, Lock, PenLine, Undo2, UserRound } from 'lucide-vue-next';

import type { OnlineUser, ShareUserOptions } from '@/types/modules/user.type';

import { getMinuteLabel } from '@/utils';
import { useSessionAdapter } from '@/hooks/useSessionAdapter';

const message = useMessage();

const {
  onlineUsers,
  shareInfo,
  userOptions,
  createShareLink,
  searchUsers,
  removeShareUser,
  copyShareURL: adapterCopyShareURL,
  resetShareState,
} = useSessionAdapter();

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
  () => shareInfo.value.shareCode,
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
  return shareInfo.value.shareURL || t('NoLink');
});

const mappedUserOptions = computed(() => {
  if (userOptions.value && userOptions.value.length > 0) {
    return userOptions.value.map((item: ShareUserOptions) => ({
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
  resetShareState();
};

const handleBack = () => {
  loading.value = false;
  showCreateForm.value = true;
  resetShareState();
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

const copyShareURLHandler = () => {
  adapterCopyShareURL();
};

const handleShareURlCreated = () => {
  if (!shareInfo.value.sessionId) {
    return message.error(t('创建连接失败'));
  }

  loading.value = true;
  createShareLink(shareLinkRequest);
};

const handleSearch = (_query: string) => {
  searchLoading.value = true;
  searchUsers(_query);

  setTimeout(() => {
    searchLoading.value = false;
  }, 500);
};

const handleRemoveShareUser = (user: OnlineUser) => {
  removeShareUser(user);
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
      <n-text depth="2" class="text-sm opacity-70"> 在线用户（{{ onlineUsers?.length || 0 }}） </n-text>
    </n-divider>

    <n-flex v-if="onlineUsers?.length" class="w-full mb-4">
      <n-list class="w-full" bordered hoverable>
        <n-list-item v-for="user in onlineUsers" :key="user.user_id">
          <template #suffix>
            <n-popconfirm
              v-if="!user.primary"
              :ok-text="t('Confirm')"
              :cancel-text="t('Cancel')"
              :negative-button-props="{
                type: 'default',
              }"
              @positive-click="handleRemoveShareUser(user)"
            >
              <template #trigger>
                <Delete
                  :size="18"
                  class="cursor-pointer hover:text-red-500 transition-all duration-200 focus:outline-none"
                />
              </template>
              <span>{{ t('RemoveUser') }}</span>
            </n-popconfirm>
          </template>

          <n-flex vertical>
            <n-text>{{ user.user }}</n-text>
            <n-flex :size="8">
              <NTag :bordered="false" size="small" :type="user.primary ? 'info' : 'success'">
                <template #icon>
                  <Crown v-if="user.primary" :size="14" />
                  <UserRound v-else :size="14" />
                </template>
                {{ user.primary ? '主用户' : '共享用户' }}
              </NTag>
              <NTag :bordered="false" :type="user.writable ? 'warning' : 'success'" size="small">
                <template #icon>
                  <PenLine v-if="user.writable" :size="14" />
                  <Lock v-else :size="14" />
                </template>
                {{ user.writable ? t('Writable') : t('ReadOnly') }}
              </NTag>
            </n-flex>
          </n-flex>
        </n-list-item>
      </n-list>

      <n-button type="primary" size="small" class="!w-full mt-1" :disabled="!shareInfo.enableShare" @click="openModal">
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
                :disabled="!shareInfo.enableShare"
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
                  <n-input readonly :loading="!shareInfo.shareCode" :value="shareInfo.shareCode" />
                </n-form-item-gi>
              </n-grid>

              <n-button type="primary" size="small" class="!w-full" @click="copyShareURLHandler">
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
