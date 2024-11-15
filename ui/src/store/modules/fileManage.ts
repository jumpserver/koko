import { defineStore } from 'pinia';

import type { IFileManageSftpFileItem } from '@/hooks/interface';

interface IFileManageStoreState {
  fileList: IFileManageSftpFileItem[] | null;
}

export const useFileManageStore = defineStore('fileManage', {
  state: (): IFileManageStoreState => ({
    fileList: null
  }),
  actions: {
    setFileList(fileList: IFileManageSftpFileItem[]) {
      if (fileList) {
        console.log('=>(fileManage.ts:15) fileList', fileList);
        this.fileList = fileList;
      }
    }
  }
});
