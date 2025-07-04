import type { InjectionKey } from 'vue';

import mitt from 'mitt';
import { inject, nextTick } from 'vue';

import type { LunaEventType } from '@/utils/lunaBus';
import type { LunaMessage, TerminalSessionInfo } from '@/types/modules/postmessage.type';

import mittBus from '@/utils/mittBus';
import { formatMessage } from '@/utils';
import { LunaCommunicator } from '@/utils/lunaBus';
import { terminalTheme } from '@/hooks/useTerminalSocket';
import { getXTerminalLineContent } from '@/hooks/helper/index';
import { useConnectionStore } from '@/store/modules/useConnection';
import { FORMATTER_MESSAGE_TYPE, LUNA_MESSAGE_TYPE } from '@/types/modules/message.type';

// 定义事件类型
type TerminalEvents = Record<string, any> & {
  'luna-event': { event: string; data: any };
  'terminal-session': TerminalSessionInfo;
  'terminal-connect': { id: string };
};

interface TerminalContext {
  eventBus: ReturnType<typeof mitt<TerminalEvents>>;

  lunaCommunicator: LunaCommunicator;

  sendLunaEvent: (event: string, data: any) => void;

  initializeLunaListeners: () => void;

  initialize: () => void;
  cleanup: () => void;
}

// 创建注入键
export const terminalContextKey: InjectionKey<TerminalContext> = Symbol('terminal-context');

// 创建 Context 实例
export const createTerminalContext = (): TerminalContext => {
  const eventBus = mitt<TerminalEvents>();
  const lunaCommunicator = new LunaCommunicator();
  const connectionStore = useConnectionStore();

  const sendLunaEvent = (event: string, data: any) => {
    eventBus.emit('luna-event', { event, data });
  };

  const initializeLunaListeners = () => {
    eventBus.on('luna-event', ({ event, data }) => {
      switch (event) {
        case LUNA_MESSAGE_TYPE.CLOSE:
        case LUNA_MESSAGE_TYPE.TERMINAL_ERROR:
          lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.CLOSE, data);
          break;
        case LUNA_MESSAGE_TYPE.SHARE_CODE_RESPONSE:
          lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.SHARE_CODE_RESPONSE, data);
          break;
        default:
          lunaCommunicator.sendLuna(event as LunaEventType, data);
      }
    });

    mittBus.on('remove-share-user', user => {
      const socket = connectionStore.socket;
      const terminalId = connectionStore.terminalId;

      if (!socket || !terminalId) {
        console.error('WebSocket connection may be closed, please refresh the page');
        return;
      }

      socket.send(
        formatMessage(
          terminalId,
          FORMATTER_MESSAGE_TYPE.TERMINAL_SHARE_USER_REMOVE,
          JSON.stringify({
            session: user.sessionId,
            user_meta: user.userMeta,
          })
        )
      );
    });

    mittBus.on('write-command', ({ type }) => {
      const terminal = connectionStore.terminal;

      if (terminal) {
        terminal.paste(type);
      }
    });

    const handLunaCommand = (msg: LunaMessage) => {
      const socket = connectionStore.socket;
      const terminalId = connectionStore.terminalId;

      if (!socket || !terminalId) {
        console.error('WebSocket connection may be closed, please refresh the page');
        return;
      }
      socket.send(formatMessage(terminalId, FORMATTER_MESSAGE_TYPE.TERMINAL_DATA, msg.data));
    };

    const handLunaFocus = (_msg: LunaMessage) => {
      const terminal = connectionStore.terminal;

      if (terminal) {
        terminal.focus();
      }
    };

    const handLunaThemeChange = (_msg: LunaMessage) => {
      const terminal = connectionStore.terminal;
      if (!terminal) return;

      const themeName = _msg.theme || 'Default';
      const theme = terminalTheme(themeName);

      nextTick(() => {
        terminal.options.theme = theme;
      });
    };

    const handleDrawerOpen = (_msg: LunaMessage) => {
      connectionStore.updateConnectionState({
        drawerOpenState: true,
      });
    };

    const handTerminalContent = (_msg: LunaMessage) => {
      const terminal = connectionStore.terminal;
      const sessionId = connectionStore.sessionId;
      const terminalId = connectionStore.terminalId;

      if (!terminal || !sessionId || !terminalId) {
        console.error('Terminal instance is not initialized');
        return;
      }

      const content = getXTerminalLineContent(10, terminal);

      const data = {
        content,
        sessionId,
        terminalId,
      };

      lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.TERMINAL_CONTENT_RESPONSE, data);
    };

    lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.OPEN, handleDrawerOpen);
    lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.CMD, handLunaCommand);
    lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.FOCUS, handLunaFocus);
    lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.TERMINAL_THEME_CHANGE, handLunaThemeChange);
    lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.TERMINAL_CONTENT, handTerminalContent);
  };

  const initialize = () => {
    initializeLunaListeners();
  };

  const cleanup = () => {
    eventBus.all.clear();
    mittBus.all.clear();
    lunaCommunicator.destroy();
  };

  return {
    eventBus,
    lunaCommunicator,
    sendLunaEvent,
    initializeLunaListeners,
    initialize,
    cleanup,
  };
};

// 获取 Context
export const useTerminalContext = () => {
  const context = inject(terminalContextKey);

  if (!context) {
    throw new Error('useTerminalContext must be used within TerminalProvider');
  }

  return context;
};
