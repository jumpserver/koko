<template>
  <CustomComponent
    ref="terminalRef"
    index-key="id"
    class="common-terminal"
    :theme-name="themeName"
    :terminal-type="terminalType"
    @event="onEvent"
    @socketData="onSocketData"
  />

  <PamFileList :settings="settings" />
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { useDialog, useMessage } from 'naive-ui';
import { useParamsStore } from '@/store/modules/params.ts';
import { useTerminalStore } from '@/store/modules/terminal.ts';

import { Terminal } from '@xterm/xterm';

import { storeToRefs } from 'pinia';
import { NMessageProvider } from 'naive-ui';
import { computed, h, markRaw, nextTick, onUnmounted, reactive, Ref, ref } from 'vue';

import xtermTheme from 'xterm-theme';
import mittBus from '@/utils/mittBus.ts';

import Share from '@/components/Share/index.vue';
import PamFileList from '@/components/pamFileList/index.vue';
import ThemeConfig from '@/components/ThemeConfig/index.vue';
import CustomComponent from '@/components/CustomTerminal/index.vue';

import {
  PersonAdd,
  ArrowBack,
  ArrowDown,
  ArrowForward,
  ArrowUp,
  PersonOutline,
  ApertureOutline,
  ShareSocialOutline,
  LockClosedOutline
} from '@vicons/ionicons5';

import type { ISettingProp, shareUser } from '@/views/interface';
import { Keyboard, Stop, Paste } from '@vicons/carbon';
import { readText } from 'clipboard-polyfill';

const paramsStore = useParamsStore();
const terminalStore = useTerminalStore();

const { t } = useI18n();

const { setting } = storeToRefs(paramsStore);

const dialog = useDialog();
const message = useMessage();

const sessionId = ref<string>('');
const themeName = ref<string>('Default');
const terminalType = ref<string>('common');
const enableShare = ref<boolean>(false);
const userOptions = ref<shareUser[]>([]);
const terminalRef: Ref<any> = ref();
const warningIntervalId = ref<number>(0);
const onlineUsersMap = reactive<{ [key: string]: any }>({});

onUnmounted(() => {
  clearInterval(warningIntervalId.value);
});

const settings = computed((): ISettingProp[] => {
  return [
    {
      label: 'ThemeConfig',
      title: t('ThemeConfig'),
      icon: ApertureOutline,
      disabled: () => false,
      click: () => {
        dialog.success({
          title: t('Theme'),
          class: 'set-theme',
          style: 'width: 50%; min-width: 810px',
          showIcon: false,
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
      label: 'Share',
      title: t('Share'),
      icon: ShareSocialOutline,
      disabled: () => !enableShare.value,
      click: () => {
        dialog.success({
          title: t('CreateLink'),
          class: 'share',
          style: 'width: 35%; min-width: 500px',
          showIcon: false,
          content: () => {
            return h(NMessageProvider, null, {
              default: () =>
                h(Share, {
                  sessionId: sessionId.value,
                  enableShare: enableShare.value,
                  userOptions: userOptions.value
                })
            });
          },
          onClose: () => resetShareDialog(),
          onMaskClick: () => resetShareDialog()
        });
        // 关闭抽屉
        mittBus.emit('open-setting');
      }
    },
    {
      label: 'User',
      title: t('User'),
      icon: PersonOutline,
      disabled: () => Object.keys(onlineUsersMap).length < 1,
      content: () =>
        Object.values(onlineUsersMap)
          .map((item: any) => {
            item.name = item.user;
            item.icon = item.writable ? markRaw(PersonAdd) : markRaw(LockClosedOutline);
            item.tip = item.writable ? t('Writable') : t('ReadOnly');
            return item;
          })
          .sort((a, b) => new Date(a.created).getTime() - new Date(b.created).getTime()),
      click: user => {
        if (user.primary) return;

        dialog.warning({
          title: '警告',
          content: t('RemoveShareUserConfirm'),
          positiveText: '确定',
          negativeText: '取消',
          onPositiveClick: () => {
            mittBus.emit('remove-share-user', {
              sessionId: sessionId.value,
              userMeta: user,
              type: 'TERMINAL_SHARE_USER_REMOVE'
            });
          }
        });
      }
    },
    {
      label: 'Keyboard',
      title: t('Hotkeys'),
      icon: Keyboard,
      content: [
        {
          name: 'Ctrl + C',
          icon: Stop,
          tip: t('Cancel'),
          click: () => {
            handleWriteData('Stop');
          }
        },
        {
          name: 'Command/Ctrl + V',
          icon: Paste,
          tip: t('Paste'),
          click: () => {
            handleWriteData('Paste');
          }
        },
        {
          name: 'Arrow Up',
          icon: ArrowUp,
          tip: t('UpArrow'),
          click: () => {
            handleWriteData('ArrowUp');
          }
        },
        {
          name: 'Arrow Down',
          icon: ArrowDown,
          tip: t('DownArrow'),
          click: () => {
            handleWriteData('ArrowDown');
          }
        },
        {
          name: 'Arrow Left',
          icon: ArrowBack,
          tip: t('LeftArrow'),
          click: () => {
            handleWriteData('ArrowLeft');
          }
        },
        {
          name: 'Arrow Right',
          icon: ArrowForward,
          tip: t('RightArrow'),
          click: () => {
            handleWriteData('ArrowRight');
          }
        }
      ],
      disabled: () => false,
      click: () => {}
    }
  ];
});

/**
 * 向终端写入快捷命令
 *
 * @param type
 */
const handleWriteData = async (type: string) => {
  if (!terminalRef.value) {
    message.error(t('No terminal instances available'));
    return;
  }

  const terminalInstance: Terminal = terminalRef.value?.terminalRef;

  if (!terminalInstance) {
    console.error('Terminal instance is not available');
    return;
  }

  switch (type) {
    case 'Paste': {
      terminalInstance.paste(await readText());
      break;
    }
    case 'Stop': {
      terminalInstance.paste('\x03');
      break;
    }
    case 'ArrowUp': {
      terminalInstance.paste('\x1b[A');
      break;
    }
    case 'ArrowDown': {
      terminalInstance.paste('\x1b[B');
      break;
    }
    case 'ArrowLeft': {
      terminalInstance.paste('\x1b[D');
      break;
    }
    case 'ArrowRight': {
      terminalInstance.paste('\x1b[C');
      break;
    }
  }

  requestAnimationFrame(() => {
    terminalInstance.focus();
  });
};

/**
 * 重置分享连接表单
 */
const resetShareDialog = () => {
  paramsStore.setShareId('');
  paramsStore.setShareCode('');
  dialog.destroyAll();
};

/**
 * 抛出到外层的 Socket message 事件处理
 *
 * @param msgType
 * @param msg
 * @param terminal
 */
const onSocketData = (msgType: string, msg: any, terminal: Terminal) => {
  switch (msgType) {
    case 'TERMINAL_SESSION': {
      const sessionInfo = JSON.parse(msg.data);
      const sessionDetail = sessionInfo.session;

      const share = sessionInfo.permission.actions.includes('share');

      if (sessionInfo.backspaceAsCtrlH) {
        const value = sessionInfo.backspaceAsCtrlH ? '1' : '0';

        terminalStore.setTerminalConfig('backspaceAsCtrlH', value);
      }

      if (sessionInfo.ctrlCAsCtrlZ) {
        const value = sessionInfo.ctrlCAsCtrlZ ? '1' : '0';

        terminalStore.setTerminalConfig('ctrlCAsCtrlZ', value);
      }

      if (setting.value.SECURITY_SESSION_SHARE && share) {
        enableShare.value = true;
      }

      sessionId.value = sessionDetail.id;
      themeName.value = sessionInfo.themeName;

      nextTick(() => {
        terminal.options.theme = xtermTheme[themeName.value];
      });
      break;
    }
    case 'TERMINAL_SHARE': {
      const data = JSON.parse(msg.data);

      paramsStore.setShareId(data.share_id);
      paramsStore.setShareCode(data.code);

      break;
    }
    case 'TERMINAL_SHARE_JOIN': {
      const data = JSON.parse(msg.data);

      const key: string = data.terminal_id;

      onlineUsersMap[key] = data;

      if (data.primary) {
        break;
      }

      message.info(`${data.user} ${t('JoinShare')}`);
      break;
    }
    case 'TERMINAL_SHARE_LEAVE': {
      const data = JSON.parse(msg.data);
      const key = data.terminal_id;

      if (onlineUsersMap.hasOwnProperty(key)) {
        delete onlineUsersMap[key];
      }

      message.info(`${data.user} ${t('LeaveShare')}`);
      break;
    }
    case 'TERMINAL_GET_SHARE_USER': {
      userOptions.value = JSON.parse(msg.data);
      break;
    }
    case 'TERMINAL_SESSION_PAUSE': {
      const data = JSON.parse(msg.data);

      message.info(`${data.user} ${t('PauseSession')}`);
      break;
    }
    case 'TERMINAL_SESSION_RESUME': {
      const data = JSON.parse(msg.data);

      message.info(`${data.user} ${t('ResumeSession')}`);
      break;
    }
    case 'TERMINAL_PERM_VALID': {
      clearInterval(warningIntervalId.value);
      message.info(`${t('PermissionValid')}`);
      break;
    }
    case 'TERMINAL_PERM_EXPIRED': {
      const data = JSON.parse(msg.data);
      const warningMsg = `${t('PermissionExpired')}: ${data.detail}`;
      message.warning(warningMsg);
      warningIntervalId.value = setInterval(() => {
        message.warning(warningMsg);
      }, 1000 * 60);
      break;
    }
    case 'CLOSE': {
      enableShare.value = false;

      // 用于删除分享的用户
      if (onlineUsersMap.hasOwnProperty(msg.id)) {
        delete onlineUsersMap[msg.id];
      }

      break;
    }
    default:
      break;
  }
};

const onEvent = (event: string, _data: any) => {
  switch (event) {
    case 'reconnect':
      Object.keys(onlineUsersMap).filter(key => {
        if (onlineUsersMap.hasOwnProperty(key)) {
          delete onlineUsersMap[key];
        }
      });
      break;
    case 'open':
      mittBus.emit('open-setting');
      break;
    case 'file':
      mittBus.emit('open-fileList');
      break;
  }
};
</script>

<style scoped lang="scss">
.common-terminal {
  :deep(.terminal-container) {
    overflow: hidden;

    .xterm-viewport {
      overflow: hidden;
    }

    .xterm-screen {
      height: calc(100vh - 20px) !important;
    }
  }
}
</style>
