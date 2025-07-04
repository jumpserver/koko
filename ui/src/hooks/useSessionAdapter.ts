import { computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { storeToRefs } from 'pinia';
import { useMessage } from 'naive-ui';
import { writeText } from 'clipboard-polyfill';

import type { OnlineUser, ShareUserOptions } from '@/types/modules/user.type';

import mittBus from '@/utils/mittBus';
import { formatMessage } from '@/utils';
import { BASE_URL } from '@/utils/config';
import { useTreeStore } from '@/store/modules/tree';
import { useParamsStore } from '@/store/modules/params';
import { useTerminalStore } from '@/store/modules/terminal';
import { useConnectionStore } from '@/store/modules/useConnection';
import { FORMATTER_MESSAGE_TYPE } from '@/types/modules/message.type';

/**
 * 会话数据适配器 - 统一普通连接和K8s连接的数据接口
 */
export function useSessionAdapter() {
  const { t } = useI18n();
  const message = useMessage();

  const treeStore = useTreeStore();
  const paramsStore = useParamsStore();
  const terminalStore = useTerminalStore();
  const connectionStore = useConnectionStore();

  const isK8sEnvironment = computed(() => {
    return window.location.pathname.includes('/k8s');
  });

  // 获取当前活跃的tab ID（K8s环境下使用）
  const currentActiveTab = computed(() => {
    return terminalStore.currentTab;
  });

  // 获取当前K8s节点信息
  const getCurrentK8sNode = () => {
    if (!isK8sEnvironment.value || !currentActiveTab.value) return null;
    return treeStore.getTerminalByK8sId(currentActiveTab.value);
  };

  // 统一的在线用户数据
  const onlineUsers = computed<OnlineUser[]>(() => {
    if (isK8sEnvironment.value) {
      const currentNode = getCurrentK8sNode();
      if (!currentNode?.onlineUsersMap || !currentActiveTab.value) return [];

      return currentNode.onlineUsersMap[currentActiveTab.value] || [];
    } else {
      return connectionStore.onlineUsers || [];
    }
  });

  const shareInfo = computed(() => {
    if (isK8sEnvironment.value) {
      const shareId = paramsStore.shareId || '';
      return {
        shareId,
        shareCode: paramsStore.shareCode || '',
        sessionId: getCurrentK8sNode()?.sessionIdMap?.get(currentActiveTab.value) || '',
        enableShare: getCurrentK8sNode()?.enableShare || false,
        shareURL: shareId ? `${BASE_URL}/luna/share/${shareId}/?code=${paramsStore.shareCode}` : '',
      };
    } else {
      const shareId = connectionStore.shareId || '';
      return {
        shareId,
        shareCode: connectionStore.shareCode || '',
        sessionId: connectionStore.sessionId || '',
        enableShare: connectionStore.enableShare || false,
        shareURL: shareId ? `${BASE_URL}/luna/share/${shareId}/?code=${connectionStore.shareCode}` : '',
      };
    }
  });

  const userOptions = computed<ShareUserOptions[]>(() => {
    if (isK8sEnvironment.value) {
      const currentNode = getCurrentK8sNode();
      return currentNode?.userOptions || [];
    } else {
      return connectionStore.userOptions || [];
    }
  });

  const connectionInfo = computed(() => {
    if (isK8sEnvironment.value) {
      const currentNode = getCurrentK8sNode();
      return {
        socket: currentNode?.socket,
        terminalId: currentNode?.id,
      };
    } else {
      return {
        socket: connectionStore.socket,
        terminalId: connectionStore.terminalId,
      };
    }
  });

  const createShareLink = (shareLinkRequest: {
    expiredTime: number;
    actionPerm: string;
    users: ShareUserOptions[];
  }) => {
    if (isK8sEnvironment.value) {
      const currentNode = getCurrentK8sNode();
      const sessionId = currentNode?.sessionIdMap?.get(currentActiveTab.value);

      if (!sessionId) {
        message.error(t('创建连接失败'));
        return;
      }

      mittBus.emit('create-share-url', {
        type: 'TERMINAL_SHARE',
        sessionId,
        shareLinkRequest,
      });
    } else {
      const { socket, terminalId } = storeToRefs(connectionStore);
      const sessionId = connectionStore.sessionId;

      if (!socket?.value || !terminalId?.value || !sessionId) {
        message.error(t('创建连接失败'));
        return;
      }

      socket.value.send(
        formatMessage(
          terminalId.value,
          FORMATTER_MESSAGE_TYPE.TERMINAL_SHARE,
          JSON.stringify({
            origin: window.location.origin,
            session: sessionId,
            users: shareLinkRequest.users,
            expired_time: shareLinkRequest.expiredTime,
            action_permission: shareLinkRequest.actionPerm,
          })
        )
      );
    }
  };

  const searchUsers = (query: string) => {
    if (isK8sEnvironment.value) {
      mittBus.emit('share-user', {
        type: 'TERMINAL_GET_SHARE_USER',
        query,
      });
    } else {
      const { socket, terminalId } = storeToRefs(connectionStore);

      if (!socket?.value || !terminalId?.value) {
        return;
      }

      socket.value.send(
        formatMessage(terminalId.value, FORMATTER_MESSAGE_TYPE.TERMINAL_GET_SHARE_USER, JSON.stringify({ query }))
      );
    }
  };

  const removeShareUser = (user: OnlineUser) => {
    if (isK8sEnvironment.value) {
      const currentNode = getCurrentK8sNode();
      const sessionId = currentNode?.sessionIdMap?.get(currentActiveTab.value);

      if (!sessionId) return;

      mittBus.emit('remove-share-user', {
        sessionId,
        userMeta: user,
        type: 'TERMINAL_SHARE_USER_REMOVE',
      });
    } else {
      if (!connectionStore.sessionId) return;

      mittBus.emit('remove-share-user', {
        sessionId: connectionStore.sessionId,
        userMeta: user,
        type: 'remove',
      });
    }
  };

  const copyShareURL = () => {
    const currentShareId = shareInfo.value.shareId;
    const currentShareCode = shareInfo.value.shareCode;
    const currentEnableShare = shareInfo.value.enableShare;

    if (!currentShareId || !currentEnableShare) return;

    const url = `${BASE_URL}/luna/share/${currentShareId}`;
    const linkTitle = t('LinkAddr');
    const codeTitle = t('VerifyCode');
    const text = `${linkTitle}: ${url}\n${codeTitle}: ${currentShareCode}`;

    writeText(text)
      .then(() => {
        message.success(t('CopyShareURLSuccess'));
      })
      .catch(e => {
        message.error(`Copy Error for ${e}`);
      });

    // 清理分享信息
    if (isK8sEnvironment.value) {
      paramsStore.setShareId('');
      paramsStore.setShareCode('');
    } else {
      connectionStore.updateConnectionState({
        shareId: '',
        shareCode: '',
      });
      paramsStore.setShareId('');
      paramsStore.setShareCode('');
    }
  };

  const resetShareState = () => {
    if (isK8sEnvironment.value) {
      paramsStore.setShareId('');
      paramsStore.setShareCode('');
    } else {
      connectionStore.updateConnectionState({
        shareId: '',
        shareCode: '',
      });
    }
  };

  return {
    shareInfo,
    onlineUsers,
    userOptions,
    connectionInfo,
    isK8sEnvironment,

    searchUsers,
    copyShareURL,
    createShareLink,
    removeShareUser,
    resetShareState,
  };
}
