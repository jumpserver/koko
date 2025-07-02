import type { Terminal } from '@xterm/xterm';
import type { ConfigProviderProps, UploadFileInfo } from 'naive-ui';
import type { Detection, Transfer, ZmodemSession } from 'zmodem-ts';

import Zmodem from 'zmodem-ts';
import { useI18n } from 'vue-i18n';
import prettyBytes from 'pretty-bytes';
import { computed, ref, type Ref } from 'vue';
import { Upload as UploadIcon } from 'lucide-vue-next';
import { createDiscreteApi, darkTheme, NUpload, NUploadTrigger, NText } from 'naive-ui';

import { MAX_TRANSFER_SIZE } from '@/utils/config';

export const useZmodem = () => {
  const { t } = useI18n();

  const fileInfo = ref<UploadFileInfo | null>(null);
  const sentryRef = ref<Zmodem.Sentry | null>(null);

  // 上传进度跟踪
  let lastPercent = -1;
  let messageShown = false;

  const configProviderPropsRef = computed<ConfigProviderProps>(() => ({
    theme: darkTheme,
  }));

  const { message, modal } = createDiscreteApi(['message', 'modal'], {
    configProviderProps: configProviderPropsRef,
  });

  /**
   * 终端进度条
   * @param {Transfer} transfer
   * @param {Terminal} terminal
   */
  const terminalProgress = (transfer: Transfer, terminal: Terminal) => {
    const detail = transfer.get_details();
    const offset = transfer.get_offset();

    const name = detail.name;
    const total = detail.size;

    let percent;

    if (total === 0 || total === offset) {
      percent = 100;
    } else {
      percent = Math.round((offset / total) * 100);
    }

    const msg = `${t('Download')} ${name}: ${prettyBytes(total)} ${percent}% `;

    terminal.write(`\r${msg}`);
  };

  /**
   * 上传文件
   * @param {ZmodemSession} startState
   * @param {Terminal} terminal
   */
  const handleUpload = (startState: ZmodemSession, terminal: Terminal) => {
    if (!fileInfo.value || !startState) {
      return true;
    }

    // 重置进度跟踪变量
    lastPercent = -1;
    messageShown = false;

    const { size } = fileInfo.value.file as File;

    if (size >= MAX_TRANSFER_SIZE) {
      const msg = `${t('ExceedTransferSize')}: ${prettyBytes(MAX_TRANSFER_SIZE)}`;
      message.error(msg);

      startState.abort();
      startState.close();

      return true;
    }

    Zmodem.Browser.send_files(startState, [fileInfo.value.file as File], {
      on_offer_response: (_obj: any, transfer: Transfer) => {
        if (transfer) {
          const detail = transfer.get_details();
          const name = detail.name;
          const total = detail.size;

          transfer.on('send_progress', (percent: number) => {
            percent = Math.round(percent);

            if (percent !== lastPercent) {
              let progressBar = '';
              const progressLength = Math.floor(percent / 2);

              for (let i = 0; i < progressLength; i++) {
                progressBar += '=';
              }
              for (let i = progressLength; i < 50; i++) {
                progressBar += ' ';
              }

              const msg = `${t('Upload')} ${name}: ${prettyBytes(total)} ${percent}% [${progressBar}]`;

              if (percent === 100 && !messageShown) {
                message.info(t('UploadEnd'), { duration: 5000 });
                messageShown = true;
              }

              terminal.write(`\r${msg}`);

              lastPercent = percent;
            }
          });
        }
      },
      on_file_complete: (obj: any) => {
        message.success(`${t('EndFileTransfer')}: ${t('UploadSuccess')} ${obj.name}`, {
          duration: 2000,
        });
      },
    })
      .then(() => {
        startState.abort();
        startState.close();
      })
      .catch((err: Error) => {
        message.error(err.message);
      });
  };

  /**
   * 获取上传文件信息
   * @param {UploadFileInfo} options.fileList
   */
  const handleFileChange = (options: { fileList: UploadFileInfo }) => {
    fileInfo.value = options.fileList;
  };

  const openUploadModal = (startState: ZmodemSession, terminal: Terminal) => {
    modal.create({
      preset: 'card',
      title: t('UploadTitle'),
      showIcon: false,
      closable: false,
      closeOnEsc: false,
      maskClosable: false,
      positiveText: t('Upload'),
      negativeText: t('Cancel'),
      negativeButtonProps: {
        type: 'tertiary',
      },
      positiveButtonProps: {
        type: 'tertiary',
      },
      onPositiveClick: async () => {
        if (!fileInfo.value) {
          message.error(t('MustSelectOneFile'));
          return false;
        }

        handleUpload(startState, terminal);
      },
      onNegativeClick: () => {
        startState.abort();
        startState.close();

        return true;
      },
      content: () => {
        // 没有必要再去写一个 vue 文件，emit 事件怪麻烦的
        return (
          <NUpload directory-dnd action="#" default-upload={false} multiple={false} onChange={handleFileChange}>
            <NUploadTrigger>
              <UploadIcon size={48} />
              <NText className="text-lg mt-3">{t('UploadTips')}</NText>
            </NUploadTrigger>
          </NUpload>
        );
      },
    });
  };

  // Sentry 在 Zmodem 中用于监控终端数据流、识别 ZMODEM 协议信号、启动文件传输会话
  const createSentry = (terminal: Terminal, socket: WebSocket, lastSendTime: Ref<Date>) => {
    const sentry = new Zmodem.Sentry({
      to_terminal: (octets: string) => {
        // 只有在非 ZMODEM 传输状态下，普通的终端数据才会被写入终端显示
        if (sentryRef.value && !sentryRef.value.get_confirmed_session()) {
          terminal.write(octets);
        } else {
          message.error(t('Failed to write to terminal'));
        }
      },
      sender: (octets: Uint8Array) => {
        lastSendTime.value = new Date();
        socket.send(new Uint8Array(octets));
      },
      on_retract: () => {},
      on_detect: (detection: Detection) => {
        // 用于确认并启动 ZMODEM 文件传输会话
        const startState = detection.confirm();

        terminal.write('\r\n');

        // sz 命令
        if (startState.type === 'send') {
          startState.on('session_end', () => {
            terminal.write('\r\n');

            startState.abort();
            startState.close();
          });

          // 打开一个 upload 对话框
          openUploadModal(startState, terminal);
        } else {
          // rz 命令
          startState.on('offer', (transfer: Transfer) => {
            const buffer: Uint8Array[] = [];
            const detail = transfer.get_details();

            // 文件大小限制
            if (detail.size >= MAX_TRANSFER_SIZE) {
              const msg = `${t('ExceedTransferSize')}: ${prettyBytes(MAX_TRANSFER_SIZE)}`;
              message.info(msg);

              transfer.skip();

              return;
            }

            // 接收文件数据
            transfer.on('input', (payload: Uint8Array) => {
              terminalProgress(transfer, terminal);
              buffer.push(new Uint8Array(payload));
            });

            // 保存文件
            transfer
              .accept()
              .then(() => {
                Zmodem.Browser.save_to_disk(buffer, detail.name);

                message.success(`${t('DownloadSuccess')}: ${detail.name}`);
                terminal.write('\r\n');
              })
              .catch((e: Error) => {
                message.error(`Error: ${e}`);
              });
          });

          startState.on('session_end', () => {
            startState.abort();
            terminal.write('\r\n');
          });

          startState.start();
        }
      },
    });

    sentryRef.value = sentry;

    return sentry;
  };

  return {
    createSentry,
  };
};
