<template>
  <n-flex class="w-full h-full px-4 py-4 !gap-0">
    <n-flex vertical justify="start" class="w-full">
      <n-card size="small" class="mb-40 !rounded-lg">
        <n-thing class="!w-1/2">
          <template #avatar>
            <n-avatar round size="medium" color="#fff" object-fit="contain" src="public/images/ChatGPT-Logo.png">
            </n-avatar>
          </template>

          <template #header> 货币 </template>

          <template #header-extra>
            <n-time :time="new Date()" />
          </template>

          <template #description>
            <Typed :options="options">
              <span class="typing"></span>
            </Typed>
          </template>

          <!-- <template #action>
            <n-button quaternary size="small" class="!w-[30px] !h-[30px]">
              <template #icon>
                <Copy :size="18" />
              </template>
            </n-button>
          </template> -->
        </n-thing>
      </n-card>

      <n-card size="small" class="mb-40 !rounded-lg">
        <n-thing class="user-message !w-1/2">
          <template #avatar>
            <n-avatar round size="medium" color="#fff" object-fit="contain" src="public/images/ChatGPT-Logo.png">
            </n-avatar>
          </template>
          <template #header> 货币 </template>

          <template #description>
            货币是为了提高交易效率而用于交换的中介商品。货币有多种形式，如贝壳粮食等自然物、金属纸张等加工品、银行卡信用卡等磁条卡、移动支付加密货币等APP。
          </template>

          <template #action>
            <n-button quaternary size="small" class="!w-[30px] !h-[30px]">
              <template #icon>
                <Copy :size="18" />
              </template>
            </n-button>
          </template>
        </n-thing>
      </n-card>
    </n-flex>
  </n-flex>
</template>

<script setup lang="ts">
import { Copy } from 'lucide-vue-next';
import { Typed } from '@duskmoon/vue3-typed-js';
import type { TypedOptions } from '@duskmoon/vue3-typed-js';

const options: TypedOptions = {
  strings: ['您好！请问有什么我可以帮助您的吗？'],
  loop: true,
  typeSpeed: 30,
  showCursor: true,
  cursorChar: '|',
  onComplete: self => {
    self.stop();
    document.querySelector('.typed-cursor')?.classList.add('hidden');
  }
};
</script>

<style scoped lang="scss">
.user-message {
  :deep(.n-thing-main) {
    .n-thing-avatar-header-wrapper {
      flex-direction: row-reverse;

      .n-thing-avatar {
        margin: 2px 0 0 12px;
      }

      .n-thing-header {
        flex-direction: row-reverse;
      }
    }
  }
}
</style>
