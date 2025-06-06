import { h, ref } from 'vue';
import { bytesHuman, wsIsActivated } from '@/utils';
import { createDiscreteApi, UploadFileInfo, darkTheme } from 'naive-ui';

import { Terminal } from '@xterm/xterm';
import { computed } from 'vue';
import { MAX_TRANSFER_SIZE } from '@/utils/config';

import Upload from '@/components/Upload/index.vue';

import ZmodemBrowser, {
  Detection,
  Sentry,
  SentryConfig,
  ZmodemSession,
  ZmodemTransfer
} from 'nora-zmodemjs/src/zmodem_browser';
import { Ref } from 'vue';
import { DialogOptions } from 'naive-ui/es/dialog/src/DialogProvider';

const { message, dialog } = createDiscreteApi(['message', 'dialog'], {
  configProviderProps: {
    theme: darkTheme
  }
});

interface IUseSentry {
  generateSentry: (ws: WebSocket, terminal: Terminal) => SentryConfig;
  createSentry: (ws: WebSocket, terminal: Terminal) => Sentry;
}

let lastPercent = -1;
let messageShown = false;

export const useSentry = (lastSendTime?: Ref<Date>, t?: any): IUseSentry => {
  const term: Ref<Terminal | null> = ref(null);
  const fileList: Ref<UploadFileInfo[]> = ref([]);
  const sentryRef: Ref<Sentry | null> = ref(null);
  const zmodeSession: Ref<ZmodemSession | null> = ref(null);
  const fileListLengthRef: Ref<number> = ref(0);

  const updateSendProgress = (name: string, total: number, percent: number) => {
    percent = Math.round(percent);

    if (percent !== lastPercent) {
      let progressBar = '';
      let progressLength = Math.floor(percent / 2);

      for (let i = 0; i < progressLength; i++) {
        progressBar += '=';
      }
      for (let i = progressLength; i < 50; i++) {
        progressBar += ' ';
      }

      let msg = `${t('Upload')} ${name}: ${bytesHuman(total)} ${percent}% [${progressBar}]`;

      if (percent === 100 && !messageShown) {
        message.info(t('UploadEnd'), { duration: 5000 });
        messageShown = true;
      }

      term.value?.write('\r' + msg);

      lastPercent = percent;
    }
  };

  /**
   * upload 的回调
   */
  const handleUpload = () => {
    const selectFile: UploadFileInfo = fileList.value[0];

    const { size } = selectFile.file as File;

    if (size >= MAX_TRANSFER_SIZE) {
      message.error(`${t('ExceedTransferSize')}: ${bytesHuman(MAX_TRANSFER_SIZE)}`);

      try {
        zmodeSession.value?.abort();
      } catch (e) {}

      return true;
    }

    if (!zmodeSession.value) return;

    const files = fileList.value.map(item => item.file as File);

    ZmodemBrowser.Browser.send_files(zmodeSession.value, files, {
      on_offer_response: (_obj: any, xfer: ZmodemTransfer) => {
        if (xfer) {
          const detail = xfer.get_details();
          const name = detail.name;
          const total = detail.size;

          xfer.on('send_progress', (percent: number) => {
            updateSendProgress(name, total, percent);
          });
        }
      },
      on_file_complete: (obj: any) => {
        message.success(`${t('EndFileTransfer')}: ${t('UploadSuccess')} ${obj.name}`, {
          duration: 2000
        });
      }
    })
      .then(() => {
        zmodeSession.value?.close();
      })
      .catch((e: Error) => {
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
          try {
            zmodeSession.value.abort();
          } catch (e) {
            return true;
          }
        }
        return true;
      }
    };
  });

  /**
   * 展示 progress 的函数
   *
   * @param xfer
   * @param terminal
   */
  const updateReceiveProgress = (xfer: ZmodemTransfer, terminal: Terminal) => {
    const detail = xfer.get_details();
    const name = detail.name;
    const total = detail.size;
    const offset = xfer.get_offset();
    let percent;
    if (total === 0 || total === offset) {
      percent = 100;
    } else {
      percent = Math.round((offset / total) * 100);
    }

    const msg = `${t('Download')} ${name}: ${bytesHuman(total)} ${percent}% `;

    terminal.write('\r' + msg);
  };

  /**
   * 处理 rz 命令
   * @param zsession
   * @param terminal
   */
  const handleSendSession = (zsession: ZmodemSession, terminal: Terminal) => {
    zmodeSession.value = zsession;

    zsession.on('session_end', () => {
      terminal.write('\r\n');

      if (zmodeSession.value) {
        fileList.value = [];
        zmodeSession.value.abort();
        zsession.close();
      }
    });

    dialog.success(dialogOptions.value);
  };

  /**
   * 处理 sz 命令
   * @param zsession
   * @param terminal
   */
  const handleReceiveSession = (zsession: ZmodemSession, terminal: Terminal) => {
    zmodeSession.value = zsession;
    zsession.on('offer', (xfer: ZmodemTransfer) => {
      const buffer: Uint8Array[] = [];
      const detail = xfer.get_details();

      if (detail.size >= MAX_TRANSFER_SIZE) {
        const msg = `${t('ExceedTransferSize')}: ${bytesHuman(MAX_TRANSFER_SIZE)}`;

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
          message.success(`${t('DownloadSuccess')}: ${detail.name}`);
          terminal.write('\r\n');
        })
        .catch((e: Error) => {
          message.error(`Error: ${e}`);
        });
    });

    zsession.on('session_end', () => {
      if (zmodeSession.value) {
        zmodeSession.value.abort();
      }
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
        console.log('Failed to write to terminal');
      }
    };

    const sender = (octets: Uint8Array) => {
      if (!wsIsActivated(ws)) {
        return;
      }

      try {
        lastSendTime && (lastSendTime.value = new Date());
        ws.send(new Uint8Array(octets));
      } catch (err) {
        console.log('Failed to send octets via WebSocket');
      }
    };

    const on_retract = () => {};

    const on_detect = (detection: Detection) => {
      const zsession: ZmodemSession = detection.confirm();

      terminal.write('\r\n');

      // @ts-ignore
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
