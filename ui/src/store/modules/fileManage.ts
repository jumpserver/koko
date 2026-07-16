import type { UploadFileInfo } from 'naive-ui';

import { defineStore } from 'pinia';

import type { FileManageSftpFileItem } from '@/types/modules/file.type';

interface IFileManageStoreState {
  fileList: FileManageSftpFileItem[] | null;

  currentPath: string;

  uploadFileList: UploadFileInfo[];
}

export const useFileManageStore = defineStore('fileManage', {
  state: (): IFileManageStoreState => ({
    fileList: null,

    currentPath: '',

    uploadFileList: [],
  }),
  actions: {
    setFileList(fileList: FileManageSftpFileItem[]) {
      if (fileList) {
        this.fileList = fileList;
      }
    },
    setCurrentPath(currentPath: string): void {
      this.currentPath = currentPath;
    },
    setUploadFileList(fileList: UploadFileInfo[]) {
      this.uploadFileList = fileList;
    },
  },
});
