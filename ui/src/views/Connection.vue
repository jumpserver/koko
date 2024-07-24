<template>
    <n-space size="large" class="w-full h-full">
        <Terminal :enable-zmodem="true" :connectURL="wsURL" />
        <n-button type="default" @click="copyShareURL">Copy</n-button>
    </n-space>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { useRoute } from 'vue-router';
import { useLogger } from '@/hooks/useLogger';
import { useMessage } from 'naive-ui';
import { copyTextToClipboard } from '@/utils';
import { BASE_URL, BASE_WS_URL } from '@/config';
import { ref, reactive, computed } from 'vue';

import Terminal from '@/components/Terminal/Terminal.vue';

const { t } = useI18n();
const { debug } = useLogger();

const route = useRoute();
const message = useMessage();

const shareId = ref(null);
const shareCode = ref(null);
const shareInfo = ref(null);

const loading = ref(false);
const userLoading = ref(false);
const enableShare = ref(false);
const dialogVisible = ref(false);
const shareDialogVisible = ref(false);

const sessionId = ref('');
const themeName = ref('Default');
const themeBackGround = ref('#1E1E1E');

const onlineUsersMap = reactive({});
const shareLinkRequest = reactive({
    expiredTime: 10,
    actionPerm: 'writable',
    users: []
});

const userOptions = reactive([]);
const expiredOptions = reactive([
    { label: getMinuteLabel(1), value: 1 },
    { label: getMinuteLabel(5), value: 5 },
    { label: getMinuteLabel(10), value: 10 },
    { label: getMinuteLabel(20), value: 20 },
    { label: getMinuteLabel(60), value: 60 }
]);
const actionsPermOptions = reactive([
    { label: t('Writable'), value: 'writable' },
    { label: t('ReadOnly'), value: 'readonly' }
]);

function getMinuteLabel(item: number) {
    // console.log(item);
    return '';
}

const wsURL = computed(() => {
    return getConnectURL();
});
const shareURL = computed(() => {
    return shareId.value ? `${BASE_URL}/koko/share/${shareId.value}/` : t('NoLink');
});

const getConnectURL = () => {
    const routeName = route.name;
    const urlParams = new URLSearchParams(window.location.search.slice(1));

    let connectURL;

    switch (routeName) {
        case 'Token':
            const params = route.params;
            const requireParams = new URLSearchParams();

            requireParams.append('type', 'token');
            requireParams.append('target_id', params.id as string);

            connectURL = BASE_WS_URL + '/koko/ws/token/?' + requireParams.toString();
            break;
        case 'TokenParams':
            connectURL = urlParams && `${BASE_WS_URL}/koko/ws/token/?${urlParams.toString()}`;
            break;
        default: {
            connectURL = urlParams && `${BASE_WS_URL}/koko/ws/terminal/?${urlParams.toString()}`;
        }
    }

    return connectURL;
};

const copyShareURL = () => {
    if (!enableShare.value) {
        return;
    }
    if (!shareId.value) {
        return;
    }

    const url = shareURL.value;
    const linkTitle = t('LinkAddr');
    const codeTitle = t('VerifyCode');
    const text = `${linkTitle}: ${shareURL}\n${codeTitle}: ${shareCode.value}`;

    copyTextToClipboard(text);

    debug(`share URL:${url}`);
    message.success(t('CopyShareURLSuccess'));
};
</script>

<style scoped lang="scss"></style>
