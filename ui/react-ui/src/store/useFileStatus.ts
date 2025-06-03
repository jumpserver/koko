import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

import type { FileItem, UploadFileItem } from '@/types/file.type';

interface FileStatus {
  loadedMessage: {
    loaded: boolean;
    token: string;
  };

  fileMessage: {
    paths: string[];
    fileList: FileItem[];
  };

  uploadFileList: UploadFileItem[];

  setLoaded: (loaded: boolean) => void;
  setToken: (token: string) => void;
  setFileMessage: (fileMessage: { paths: string[]; fileList: FileItem[] }) => void;
  setUploadFileList: (uploadFileList: UploadFileItem[]) => void;
  clearUploadFileList: () => void;

  resetFileMessage: () => void;
  resetLoadedMessage: () => void;
}

export const useFileStatus = create(
  persist<FileStatus>(
    set => ({
      loadedMessage: {
        loaded: false,
        token: ''
      },

      fileMessage: {
        paths: [],
        fileList: []
      },

      uploadFileList: [],

      setLoaded: (loaded: boolean) => set(state => ({ loadedMessage: { ...state.loadedMessage, loaded } })),
      setToken: (token: string) => set(state => ({ loadedMessage: { ...state.loadedMessage, token } })),

      setFileMessage: (_fileMessage: { paths: string[]; fileList: FileItem[] }) =>
        set(state => {
          // 过滤出 fileMessage 中已经有的 path
          const newPath = _fileMessage.paths.filter(item => !state.fileMessage.paths.includes(item));

          return {
            fileMessage: {
              paths: [...state.fileMessage.paths, ...newPath],
              fileList: _fileMessage.fileList
            }
          };
        }),
      setUploadFileList: (uploadFileList: UploadFileItem[]) =>
        set(state => ({ uploadFileList: [...state.uploadFileList, ...uploadFileList] })),
      clearUploadFileList: () => set(() => ({ uploadFileList: [] })),

      resetFileMessage: () => set(() => ({ fileMessage: { paths: [], fileList: [] } })),
      resetLoadedMessage: () => set(() => ({ loadedMessage: { loaded: false, token: '' } }))
    }),
    {
      name: 'KOKO_USER_FILE_STATUS',
      storage: createJSONStorage(() => localStorage)
    }
  )
);
