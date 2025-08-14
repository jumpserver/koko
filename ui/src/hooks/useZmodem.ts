import type { Ref } from 'vue';
import type { Terminal } from '@xterm/xterm';
import type { ConfigProviderProps, UploadFileInfo } from 'naive-ui';
import type { Detection, Transfer, ZmodemSession } from 'nora-zmodemjs/src/zmodem_browser';

import { useI18n } from 'vue-i18n';
import { computed, h, ref } from 'vue';
import prettyBytes from 'pretty-bytes';
import { createDiscreteApi, darkTheme } from 'naive-ui';
import ZmodemBrowser from 'nora-zmodemjs/src/zmodem_browser';

import { MAX_TRANSFER_SIZE } from '@/utils/config';
import ZmodemUpload from '@/components/ZmodemUpload/index.vue';

export const useZmodem = () => {
  const { t } = useI18n();

  const fileInfo = ref<File | null>(null);
  const sentryRef = ref<ZmodemBrowser.Sentry | null>(null);
  const activeSession = ref<ZmodemSession | null>(null);

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
   * 清理 session 状态
   */
  const cleanupSession = () => {
    if (activeSession.value) {
      try {
        activeSession.value.close();
      } catch (e) {
        console.warn('Error cleaning up session:', e);
      }
      activeSession.value = null;
    }
    // 重置进度跟踪变量
    lastPercent = -1;
    messageShown = false;
  };

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
   * @param {ZmodemSession} session
   * @param {Terminal} terminal
   */
  const handleUpload = (session: ZmodemSession, terminal: Terminal) => {
    if (!fileInfo.value || !session) {
      return;
    }

    // 重置进度跟踪变量
    lastPercent = -1;
    messageShown = false;

    const { size } = fileInfo.value as File;

    if (size >= MAX_TRANSFER_SIZE) {
      const msg = `${t('ExceedTransferSize')}: ${prettyBytes(MAX_TRANSFER_SIZE)}`;
      message.error(msg);
      cleanupSession();
      return;
    }

    ZmodemBrowser.Browser.send_files(session, [fileInfo.value], {
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
        cleanupSession();
      })
      .catch((err: Error) => {
        message.error(err.message);
        cleanupSession();
        activeSession.value?.abort();
      });
  };

  /**
   * 获取上传文件信息
   * @param {UploadFileInfo} options.fileList
   */
  const handleFileChange = (options: { fileList: UploadFileInfo[] }) => {
    fileInfo.value = options.fileList[0].file as File;
  };

  /**
   * 处理发送会话 (rz 命令)
   * @param {ZmodemSession} session
   * @param {Terminal} terminal
   */
  const handleSendSession = (session: ZmodemSession, terminal: Terminal) => {
    activeSession.value = session;

    session.on('session_end', () => {
      terminal.write('\r\n');
      cleanupSession();
    });

    // 打开上传对话框
    modal.create({
      preset: 'dialog',
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

        handleUpload(session, terminal);
        return true;
      },
      onNegativeClick: () => {
        cleanupSession();

        terminal.write('\r\n');
        return true;
      },
      content: () => {
        return h(ZmodemUpload, {
          t,
          onFileChange: handleFileChange,
        });
      },
    });
  };

  /**
   * 处理接收会话 (sz 命令)
   * @param {ZmodemSession} session
   * @param {Terminal} terminal
   */
  const handleReceiveSession = (session: ZmodemSession, terminal: Terminal) => {
    activeSession.value = session;

    session.on('offer', (transfer: Transfer) => {
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
          ZmodemBrowser.Browser.save_to_disk(buffer, detail.name);
          message.success(`${t('DownloadSuccess')}: ${detail.name}`);
          terminal.write('\r\n');
        })
        .catch((e: Error) => {
          message.error(`Error: ${e}`);
        });
    });

    session.on('session_end', () => {
      terminal.write('\r\n');
      cleanupSession();
    });

    session.start();
  };

  // Sentry 在 Zmodem 中用于监控终端数据流、识别 ZMODEM 协议信号、启动文件传输会话
  const createSentry = (terminal: Terminal, socket: WebSocket, lastSendTime: Ref<Date>) => {
    const sentry = new ZmodemBrowser.Sentry({
      to_terminal: (octets: string) => {
        try {
          // 只有在没有确认的 session 时，普通的终端数据才会被写入终端显示
          if (sentryRef.value && !sentryRef.value.get_confirmed_session()) {
            terminal.write(octets);
          }
        } catch (_e) {
          message.error(t('Failed to write to terminal'));
        }
      },
      sender: (octets: Uint8Array) => {
        try {
          lastSendTime.value = new Date();
          socket.send(new Uint8Array(octets));
        } catch (_e) {
          console.warn('Failed to send octets via WebSocket');
        }
      },
      on_retract: () => {},
      on_detect: (detection: Detection) => {
        try {
          // 直接确认检测到的 ZMODEM 会话
          const session = detection.confirm();

          terminal.write('\r\n');

          // 根据会话类型处理
          if (session.type === 'send') {
            // rz 命令 - 上传
            handleSendSession(session, terminal);
          } else {
            // sz 命令 - 下载
            handleReceiveSession(session, terminal);
          }
        } catch (error) {
          console.warn('Error in ZMODEM detection:', error);
          cleanupSession();
          activeSession.value?.abort();
        }
      },
    });

    sentryRef.value = sentry;

    return sentry;
  };

  return {
    createSentry,
    cleanupSession,
  };
};
