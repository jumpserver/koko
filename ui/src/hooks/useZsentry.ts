import ZmodemBrowser, {
  Detection,
  SentryConfig,
  ZmodemSession
} from 'nora-zmodemjs/src/zmodem_browser';
import { Terminal } from '@xterm/xterm';
import { wsIsActivated } from '@/components/Terminal/helper';
import { useLogger } from '@/hooks/useLogger.ts';
import { Ref } from 'vue';

const { debug, info } = useLogger('useZsentry');

export const useZsentry = () => {
  const generateZsentry = (
    ws: WebSocket,
    terminal: Terminal,
    lastSendTime: Ref<Date>,
    zsentryRef: ZmodemBrowser.Sentry
  ): SentryConfig => {
    return {
      // 将数据写入终端。
      to_terminal: (octets: string) => {
        if (zsentryRef && !zsentryRef.get_confirmed_session()) return terminal.write(octets);
      },

      // 将数据通过 WebSocket 发送
      sender: (octets: Uint8Array) => {
        if (!wsIsActivated(ws)) return debug('WebSocket Closed');

        lastSendTime.value = new Date();

        debug(`octets: ${octets}`);
        ws.send(new Uint8Array(octets));
      },

      // 处理 Zmodem 撤回事件
      on_retract: () => info('Zmodem Retract'),

      // 处理检测到的 Zmodem 会话
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

  const createZsentry = (
    ws: WebSocket,
    terminal: Terminal,
    zsentryRef: ZmodemBrowser.Sentry,
    lastSendTime: Ref<Date>
  ) => {
    const zsentryConfig = generateZsentry(ws, terminal, lastSendTime, zsentryRef);

    return new ZmodemBrowser.Sentry(zsentryConfig);
  };

  return {
    createZsentry
  };
};
