import type { UploadFileInfo } from 'naive-ui';

import type { FileManageSftpFileItem } from '@/types/modules/file.type';
import { defineStore } from 'pinia';

interface IFileManageStoreState {
  fileList: FileManageSftpFileItem[] | null;

  messageId: string;

  currentPath: string;

  isReceived: boolean;

  uploadFileList: UploadFileInfo[];
}

export const useFileManageStore = defineStore('fileManage', {
  state: (): IFileManageStoreState => ({
    fileList: null,

    messageId: '',

    currentPath: '',

    isReceived: false,

    uploadFileList: [],
  }),
  actions: {
    setFileList(fileList: FileManageSftpFileItem[]) {
      if (fileList) {
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
    },
    setUploadFileList(fileList: UploadFileInfo[]) {
      this.uploadFileList = fileList;
    },
  },
});
