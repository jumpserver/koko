import router from '../index';

import { storeToRefs } from 'pinia';
import { createDiscreteApi } from 'naive-ui';
import { useGlobalStore } from '@/store/modules/global';
import type { RouteLocationNormalized, NavigationGuardNext } from 'vue-router';

const { message } = createDiscreteApi(['message']);

const onI18nLoaded = () => {
  const globalStore = useGlobalStore();
  const { i18nLoaded } = storeToRefs(globalStore);

  return new Promise(resolve => {
    if (i18nLoaded) {
      resolve(true);
    }

    const itv = setInterval(() => {
      if (i18nLoaded) {
        clearInterval(itv);
        resolve(true);
      }
    }, 100);
  });
};

/**
 * @description 启动函数
 * @returns
 */
const startUp = async (): Promise<boolean> => {
  const globalStore = useGlobalStore();
  const { inited } = storeToRefs(globalStore);

  if (inited) {
    return true;
  }

  await globalStore.init();
  await onI18nLoaded();

  return true;
};

router.beforeEach(
  async (to: RouteLocationNormalized, from: RouteLocationNormalized, next: NavigationGuardNext) => {
    try {
      await startUp();
      next();
    } catch (e) {
      message.error(`Start service error: ${e}`);
    }
  }
);
