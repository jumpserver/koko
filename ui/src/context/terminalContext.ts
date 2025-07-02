import type { InjectionKey } from 'vue';

import mitt from 'mitt';
import { inject, nextTick } from 'vue';

import type { LunaEventType } from '@/utils/lunaBus';
import type { LunaMessage, TerminalSessionInfo } from '@/types/modules/postmessage.type';

import { formatMessage } from '@/utils';
import { LunaCommunicator } from '@/utils/lunaBus';
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
      // TODO 这里需要导入 terminalTheme 函数，暂时简化处理
      nextTick(() => {
        // TODO terminal.options.theme = terminalTheme(themeName);
        console.warn('Theme change:', themeName);
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

      // TODO 简化获取终端内容的逻辑
      const getXTerminalLineContent = (index: number) => {
        const buffer = terminal.buffer.active;
        if (!buffer) return '';

        const result: string[] = [];
        const bufferLineCount = buffer.length;
        let startLine = bufferLineCount;

        while (result.length < index || startLine >= 0) {
          startLine--;
          if (startLine < 0) break;

          const line = buffer.getLine(startLine);
          if (!line) {
            console.warn(`Line ${startLine} is empty or undefined`);
            continue;
          }
          result.unshift(line.translateToString());
        }
        return result.join('\n');
      };

      const content = getXTerminalLineContent(10);
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
