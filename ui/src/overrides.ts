import type { GlobalThemeOverrides } from 'naive-ui';

export const themeOverrides: GlobalThemeOverrides = {
  Drawer: {
    color: '#121515',
    titleTextColor: '#EBEBEB'
  },
  Form: {},
  Select: {
    peers: {
      InternalSelectMenu: {
        color: '#191D1D'
      }
    }
  },
  DataTable: {
    thColorModal: 'unset'
  }
};
