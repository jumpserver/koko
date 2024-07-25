import { storeToRefs } from 'pinia';
import { createDiscreteApi } from 'naive-ui';
import { useGlobalStore } from '@/store/modules/global';
import type { RouteLocationNormalized, NavigationGuardNext } from 'vue-router';

const { message } = createDiscreteApi(['message']);

const onI18nLoaded = () => {
    const globalStore = useGlobalStore();
    const { i18nLoaded } = storeToRefs(globalStore);

    return new Promise(resolve => {
        if (i18nLoaded.value) {
            message.success('i18n already loaded');
            resolve(true);
        }

        const itv = setInterval(() => {
            if (i18nLoaded.value) {
                clearInterval(itv);
                message.info('i18n loaded after interval');
                resolve(true);
            }
        }, 100);
    });
};

const startUp = async (): Promise<boolean> => {
    const globalStore = useGlobalStore();
    const { inited } = storeToRefs(globalStore);

    if (inited.value) {
        message.success('Already inited');
        return true;
    }

    message.info('Initializing global store');

    globalStore.init();
    await onI18nLoaded();

    return true;
};

export const guard = async (
    to: RouteLocationNormalized,
    from: RouteLocationNormalized,
    next: NavigationGuardNext
) => {
    try {
        await startUp();
        console.log(to, from);
        next();
    } catch (error) {
        message.error(`Start service error: ${error}`);
    }
};
