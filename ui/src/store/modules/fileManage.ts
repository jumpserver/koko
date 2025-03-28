import { defineStore } from 'pinia';

import type { IFileManageSftpFileItem } from '@/hooks/interface';

interface IFileManageStoreState {
  fileList: IFileManageSftpFileItem[] | null;

  messageId: string;

  currentPath: string;

  isReceived: boolean;
}

export const useFileManageStore = defineStore('fileManage', {
  state: (): IFileManageStoreState => ({
    fileList: null,

    messageId: '',

    currentPath: '',

    isReceived: false
  }),
  actions: {
    setFileList(fileList: IFileManageSftpFileItem[]) {
      if (fileList) {
        console.log(
          '%c DEBUG[ fileList ]-72:',
          'font-size:13px; background: #1ab394; color:#fff;',
          fileList
        );
        this.fileList = fileList;
      }
    },
    setMessageId(id: string): void {
      this.messageId = id;
    },
    setCurrentPath(currentPath: string): void {
      this.currentPath = currentPath;
    },
    setReceived(value: boolean) {
      this.isReceived = value;
    }
  }
});
