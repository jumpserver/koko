import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

import type { FileItem } from '@/types/file.type';

interface FileStatus {
  loadedMessage: {
    loaded: boolean;
    token: string;
  };
  fileMessage: {
    paths: string[];
    fileList: FileItem[];
  };

  setLoaded: (loaded: boolean) => void;
  setToken: (token: string) => void;
  setFileMessage: (fileMessage: { paths: string; fileList: FileItem[] }) => void;
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

      // 单独设置 loaded
      setLoaded: (loaded: boolean) => set(state => ({ loadedMessage: { ...state.loadedMessage, loaded } })),
      setToken: (token: string) => set(state => ({ loadedMessage: { ...state.loadedMessage, token } })),

      setFileMessage: (_fileMessage: { paths: string; fileList: FileItem[] }) =>
        set(state => {
          let newPath: string[];

          const pathExists = state.fileMessage.paths.includes(_fileMessage.paths);

          if (pathExists) {
            // 如果存在这个目录那么直接就截取到那个路径下
            const index = state.fileMessage.paths.indexOf(_fileMessage.paths);
            newPath = state.fileMessage.paths.slice(0, index + 1);
          } else {
            newPath = [...state.fileMessage.paths, _fileMessage.paths];
          }

          return {
            fileMessage: {
              paths: newPath,
              fileList: _fileMessage.fileList
            }
          };
        })
    }),
    {
      name: 'KOKO_USER_FILE_STATUS',
      storage: createJSONStorage(() => localStorage)
    }
  )
);
