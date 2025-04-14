import type { GlobalThemeOverrides } from 'naive-ui';

export const themeOverrides: GlobalThemeOverrides = {
  Drawer: {},
  DataTable: {
    thColorModal: 'unset'
  },
  Upload: {
    peers: {
      Progress: {
        fillColor: '#16987D',
        fillColorInfo: '#16987D',
      }
    }
  }
};
