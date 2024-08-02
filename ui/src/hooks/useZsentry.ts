import { Ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { useLogger } from '@/hooks/useLogger.ts';
import { wsIsActivated } from '@/components/Terminal/helper';

import ZmodemBrowser, {
  Detection,
  Sentry,
  SentryConfig,
  ZmodemSession
} from 'nora-zmodemjs/src/zmodem_browser';

const { debug, info } = useLogger('useSentry');

interface SentryHook {
  generateSentry: (
    ws: WebSocket,
    terminal: Terminal,
    sentryRef: ZmodemBrowser.Sentry
  ) => SentryConfig;
  createSentry: (
    ws: WebSocket,
    terminal: Terminal,
    sentryRef: Ref<ZmodemBrowser.Sentry | null>
  ) => Sentry;
}

export const useSentry = (lastSendTime: Ref<Date>): SentryHook => {
  /**
   * 生成 ZSentry 配置
   * @param ws WebSocket 实例
   * @param terminal xterm.js 终端实例
   * @param sentryRef ZmodemBrowser.Sentry 引用
   */
  const generateSentry = (
    ws: WebSocket,
    terminal: Terminal,
    sentryRef: ZmodemBrowser.Sentry
  ): SentryConfig => {
    return {
      to_terminal: (octets: string) => {
        if (sentryRef && !sentryRef.get_confirmed_session()) return terminal.write(octets);
      },
      sender: (octets: Uint8Array) => {
        if (!wsIsActivated(ws)) return debug('WebSocket Closed');
        lastSendTime.value = new Date();
        debug(`octets: ${octets}`);
        ws.send(new Uint8Array(octets));
      },
      on_retract: () => info('Zmodem Retract'),
      on_detect: (detection: Detection) => {
        const zsession: ZmodemSession = detection.confirm();
        terminal.write('\r\n');
        if (zsession.type === 'send') {
          // handleSendSession(zsession);
        } else {
          // handleReceiveSession(zsession);
        }
      }
    };
  };

  /**
   * 创建 ZSentry 实例
   * @param ws WebSocket 实例
   * @param terminal xterm.js 终端实例
   * @param sentryRef ZmodemBrowser.Sentry 引用
   */
  const createSentry = (
    ws: WebSocket,
    terminal: Terminal,
    sentryRef: Ref<ZmodemBrowser.Sentry | null>
  ): Sentry => {
    const sentryConfig: SentryConfig = generateSentry(
      ws,
      terminal,
      sentryRef.value as ZmodemBrowser.Sentry
    );
    const sentryInstance = new ZmodemBrowser.Sentry(sentryConfig);
    sentryRef.value = sentryInstance;
    return sentryInstance;
  };

  return {
    createSentry,
    generateSentry
  };
};
