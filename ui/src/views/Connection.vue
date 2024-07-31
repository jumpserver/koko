<template>
  <Terminal
    ref="terminalRef"
    :enable-zmodem="true"
    :connectURL="wsURL"
    :share-code="shareCode"
    :theme-name="themeName"
    @event="onEvent"
    @ws-data="onWsData"
  />

  <Settings :settings="settings" />
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { useRoute } from 'vue-router';
import { useLogger } from '@/hooks/useLogger';
import { useTerminal } from '@/hooks/useTerminal.ts';
import { useDialog, useMessage } from 'naive-ui';
import { copyTextToClipboard } from '@/utils';
import { BASE_URL, BASE_WS_URL } from '@/config';
import { computed, h, nextTick, onMounted, reactive, ref } from 'vue';
import { ApertureOutline, PersonOutline, ShareSocialOutline } from '@vicons/ionicons5';

import mittBus from '@/utils/mittBus.ts';
import Settings from '@/components/Settings/index.vue';
import Terminal from '@/components/Terminal/Terminal.vue';
import ThemeConfig from '@/components/ThemeConfig/index.vue';

import { ISettingProp } from '@/views/interface';

const { setTerminalTheme } = useTerminal();

const { t } = useI18n();
const { debug } = useLogger('Connection');

const route = useRoute();
const message = useMessage();

const shareInfo = ref(null);
const dialogVisible = ref(false);
const shareDialogVisible = ref(false);

const terminalRef = ref(null);
const sessionId = ref('');
const themeBackGround = ref('#1E1E1E');
const shareLinkRequest = reactive({
  expiredTime: 10,
  actionPerm: 'writable',
  users: []
});

const shareId = ref(null);
const shareCode = ref<any>();
const loading = ref(false);
const userLoading = ref(false);
const enableShare = ref(false);
const themeName = ref('Default');
const onlineUsersMap = reactive<{ [key: string]: any }>({});

const userOptions = ref(null);
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

const dialog = useDialog();

const wsURL = computed(() => {
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
});
const shareURL = computed(() => {
  return shareId.value ? `${BASE_URL}/koko/share/${shareId.value}/` : t('NoLink');
});
const settings = computed((): ISettingProp[] => {
  return [
    {
      title: t('ThemeConfig'),
      icon: ApertureOutline,
      disabled: () => false,
      click: () => {
        dialog.success({
          class: 'set-theme',
          title: t('Theme'),
          showIcon: false,
          style: 'width: 50%',
          content: () =>
            h(ThemeConfig, {
              currentThemeName: themeName.value,
              preview: (tempTheme: string) => {
                themeName.value = tempTheme;
              }
            })
        });
        // 关闭抽屉
        mittBus.emit('open-setting');
      }
    },
    {
      title: t('Share'),
      icon: ShareSocialOutline,
      disabled: () => !enableShare.value,
      click: () => {}
    },
    {
      title: t('User'),
      icon: PersonOutline,
      disabled: () => Object.keys(onlineUsersMap).length < 1,
      content: Object.values(onlineUsersMap)
        .map((item: any) => {
          item.name = item.user;
          item.faIcon = item.writable ? 'fa-solid fa-keyboard' : 'fa-solid fa-eye';
          item.iconTip = item.writable ? t('Writable') : t('ReadOnly');
          return item;
        })
        .sort((a, b) => new Date(a.created).getTime() - new Date(b.created).getTime()),
      click: () => {}
    }
  ];
});

function getMinuteLabel(item: number) {
  // console.log(item);
  return '';
}

const copyShareURL = (msgType, msg) => {
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
const onWsData = (msgType: string, msg: any, terminal: Terminal) => {
  const data = JSON.parse(msg.data);

  debug('msgType:', msgType, 'onWsData:', data);

  switch (msgType) {
    case 'TERMINAL_SESSION': {
      const sessionInfo = data;
      const sessionDetail = sessionInfo.session;

      debug(`SessionDetail themeName: ${sessionInfo.themeName}`);
      debug(`SessionDetail permissions: ${sessionInfo.permission}`);
      debug(`SessionDetail ctrlCAsCtrlZ: ${sessionInfo.ctrlCAsCtrlZ}`);
      debug(`SessionDetail backspaceAsCtrlH: ${sessionInfo.backspaceAsCtrlH}`);

      const enableShare = sessionInfo.permission.actions.includes('share');

      if (sessionInfo.backspaceAsCtrlH) {
        const value = sessionInfo.backspaceAsCtrlH ? '1' : '0';
        debug(`Set backspaceAsCtrlH: ${value}`);

        terminal.options.backspaceAsCtrlH = value;
      }

      if (sessionInfo.ctrlCAsCtrlZ) {
        const value = sessionInfo.ctrlCAsCtrlZ ? '1' : '0';
        debug(`Set ctrlCAsCtrlZ: ${value}`);

        terminal.options.ctrlCAsCtrlZ = value;
      }

      sessionId.value = sessionDetail.id;
      themeName.value = sessionInfo.themeName;

      nextTick(() => {
        setTerminalTheme(themeName.value, terminal);
      });

      break;
    }
    case 'TERMINAL_SHARE': {
      shareId.value = data.share_id;
      shareCode.value = data.code;

      loading.value = false;
      break;
    }
    case 'TERMINAL_SHARE_JOIN': {
      const key: string = data.terminal_id;

      onlineUsersMap[key] = data;

      debug('onlineUsersMap', onlineUsersMap);

      if (data.primary) {
        debug('Primary User 不提醒');
        break;
      }

      message.info(`${data.user} ${t('JoinShare')}`);
      break;
    }
    case 'TERMINAL_SHARE_LEAVE': {
      const key = data.terminal_id;

      if (onlineUsersMap.hasOwnProperty(key)) {
        delete onlineUsersMap[key]; // 确保删除属性时响应式更新
      }

      message.info(`${data.user} ${t('LeaveShare')}`);
      break;
    }
    case 'TERMINAL_GET_SHARE_USER': {
      userLoading.value = false;
      userOptions.value = data;
      break;
    }
    case 'TERMINAL_SESSION_PAUSE': {
      message.info(`${data.user} ${t('PauseSession')}`);
      break;
    }
    case 'TERMINAL_SESSION_RESUME': {
      message.info(`${data.user} ${t('ResumeSession')}`);
      break;
    }
    default:
      break;
  }

  debug('On WebSocket Data:', msg);
};
const onEvent = (event: string, data: any) => {
  switch (event) {
    case 'reconnect':
      debug('Reconnect');
      // Object.keys(onlineUsersMap.value).filter(key => {
      //
      // });
      break;
    case 'open':
      debug('Open');
      mittBus.emit('open-setting');
      break;
  }
};

onMounted(() => {});
</script>

<style scoped lang="scss"></style>
