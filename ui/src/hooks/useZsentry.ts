// 引入 API
import { h, ref } from 'vue';
import { useLogger } from '@/hooks/useLogger.ts';
import { bytesHuman } from '@/utils';
import { wsIsActivated } from '@/components/Terminal/helper';
import { createDiscreteApi, UploadFileInfo } from 'naive-ui';

import { Terminal } from '@xterm/xterm';
import { computed } from 'vue';
import { MAX_TRANSFER_SIZE } from '@/config';

// 引入组件
import Upload from '@/components/Upload/index.vue';

// 引入类型定义
import ZmodemBrowser, {
  Detection,
  Sentry,
  SentryConfig,
  ZmodemSession,
  ZmodemTransfer
} from 'nora-zmodemjs/src/zmodem_browser';
import { Ref } from 'vue';
import { DialogOptions } from 'naive-ui/es/dialog/src/DialogProvider';

// API 初始化
const { message, dialog } = createDiscreteApi(['message', 'dialog']);
const { debug, info, error } = useLogger('useSentry');

interface IUseSentry {
  generateSentry: (ws: WebSocket, terminal: Terminal) => SentryConfig;
  createSentry: (ws: WebSocket, terminal: Terminal) => Sentry;
}

export const useSentry = (lastSendTime?: Ref<Date>, t?: any): IUseSentry => {
  const term: Ref<Terminal | null> = ref(null);
  const fileList: Ref<UploadFileInfo[]> = ref([]);
  const sentryRef: Ref<Sentry | null> = ref(null);
  const zmodeSession: Ref<ZmodemSession | null> = ref(null);
  const fileListLengthRef: Ref<number> = ref(0);

  const updateSendProgress = (xfer: ZmodemTransfer, percent: number) => {
    let detail = xfer.get_details();
    let name = detail.name;
    let total = detail.size;
    percent = Math.round(percent);

    if (term.value) {
      term.value.write('\r' + `${t('Upload')} ${name}: ${bytesHuman(total)} ${percent}%`);
    }
  };

  const handleUpload = () => {
    const selectFile: UploadFileInfo = fileList.value[0];

    const { size } = selectFile.file as File;

    if (size >= MAX_TRANSFER_SIZE) {
      debug(`Select File: ${selectFile}`);

      return message.info(`${t('ExceedTransferSize')}: ${bytesHuman(MAX_TRANSFER_SIZE)}`);
    }

    if (!zmodeSession.value) return;

    debug(`Zomdem submit file: ${selectFile.file}`);

    ZmodemBrowser.Browser.send_files(zmodeSession.value, selectFile.file as File, {
      on_offer_response: (_obj: any, xfer: ZmodemTransfer) => {
        if (xfer) {
          xfer.on('send_progress', (percent: number) => {
            updateSendProgress(xfer, percent);
          });
        }
      },
      on_file_complete: (obj: any) => {
        debug(`File Complete: ${obj}`);
        message.info(`${t('UploadSuccess')} ${obj.name}`);
      }
    })
      .then(() => {
        zmodeSession.value && zmodeSession.value.close();
      })
      .catch((e: Error) => {
        // todo)) 现在上传文件会走到这里
        console.log(e);
      });
  };

  const dialogOptions = computed((): DialogOptions => {
    return {
      class: 'zsession',
      title: t('UploadTitle'),
      showIcon: false,
      closable: false,
      closeOnEsc: false,
      maskClosable: false,
      positiveText: t('Upload'),
      negativeText: t('Cancel'),
      negativeButtonProps: {
        type: 'tertiary',
        round: true
      },
      positiveButtonProps: {
        type: 'tertiary',
        round: true
      },
      content: () =>
        h(Upload, {
          t,
          fileList,
          fileListLengthRef
        }),
      onPositiveClick: async () => {
        if (fileListLengthRef.value === 0) {
          message.error(t('MustSelectOneFile'));
          return false;
        } else {
          handleUpload();
          return true;
        }
      },
      onNegativeClick: () => {
        if (zmodeSession.value) {
          debug('Cancel Abort');
          zmodeSession.value.abort();
        }

        debug('删除 Dialog 的文件');
      }
    };
  });

  const updateReceiveProgress = (xfer: ZmodemTransfer, terminal: Terminal) => {
    let detail = xfer.get_details();
    let name = detail.name;
    let total = detail.size;
    let offset = xfer.get_offset();
    let percent;
    if (total === 0 || total === offset) {
      percent = 100;
    } else {
      percent = Math.round((offset / total) * 100);
    }

    let msg = `${t('Download')} ${name}: ${bytesHuman(total)} ${percent}% `;

    terminal.write(msg);
  };

  const handleSendSession = (zsession: ZmodemSession, terminal: Terminal) => {
    zmodeSession.value = zsession;

    zsession.on('session_end', () => {
      zmodeSession.value = null;
      fileList.value = [];
      terminal.write('\r\n');
    });

    dialog.success(dialogOptions.value);
  };

  const handleReceiveSession = (zsession: ZmodemSession, terminal: Terminal) => {
    zsession.on('offer', (xfer: ZmodemTransfer) => {
      const buffer: Uint8Array[] = [];
      const detail = xfer.get_details();

      if (detail.size >= MAX_TRANSFER_SIZE) {
        const msg = `${t('ExceedTransferSize')}: ${bytesHuman(MAX_TRANSFER_SIZE)}`;

        debug(msg);
        message.info(msg);

        xfer.skip();

        return;
      }

      xfer.on('input', (payload: Uint8Array) => {
        updateReceiveProgress(xfer, terminal);
        buffer.push(new Uint8Array(payload));
      });

      xfer
        .accept()
        .then(() => {
          ZmodemBrowser.Browser.save_to_disk(buffer, xfer.get_details().name);
          message.info(`${t('DownloadSuccess')}: ${detail.name}`);

          terminal.write('\r\n');

          if (zmodeSession.value) zmodeSession.value.abort();
        })
        .catch((e: Error) => {
          message.error(`Error: ${e}`);
        });
    });

    zsession.on('session_end', () => {
      terminal.write('\r\n');
    });

    zsession.start();
  };

  const generateSentry = (ws: WebSocket, terminal: Terminal): SentryConfig => {
    const to_terminal = (_octets: string) => {
      try {
        if (sentryRef.value && !sentryRef.value.get_confirmed_session()) {
          terminal.write(_octets);
        }
      } catch (err) {
        error('Failed to write to terminal', err);
      }
    };

    const sender = (octets: Uint8Array) => {
      if (!wsIsActivated(ws)) {
        return debug('WebSocket Closed');
      }
      try {
        lastSendTime && (lastSendTime.value = new Date());
        ws.send(new Uint8Array(octets));
      } catch (err) {
        error('Failed to send octets via WebSocket', err);
      }
    };

    const on_retract = () => info('Zmodem Retract');

    const on_detect = (detection: Detection) => {
      const zsession: ZmodemSession = detection.confirm();

      terminal.write('\r\n');

      if (zsession.type === 'send') {
        handleSendSession(zsession, terminal);
      } else {
        handleReceiveSession(zsession, terminal);
      }
    };

    return { to_terminal, sender, on_retract, on_detect };
  };

  const createSentry = (ws: WebSocket, terminal: Terminal): Sentry => {
    const sentryConfig: SentryConfig = generateSentry(ws, terminal);

    term.value = terminal;

    const sentry = new ZmodemBrowser.Sentry(sentryConfig);
    sentryRef.value = sentry;

    return sentry;
  };

  return { createSentry, generateSentry };
};
