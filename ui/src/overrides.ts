import type { GlobalThemeOverrides } from 'naive-ui';

export const themeOverrides: GlobalThemeOverrides = {
  Drawer: {
    color: '#121515',
    titleTextColor: '#EBEBEB'
  },
  Form: {},
  Select: {
    peers: {
      InternalSelection: {
        borderHover: '1px solid #16987D',
        borderActive: '1px solid #16987D',
        borderFocus: '1px solid #16987D'
      },
      InternalSelectMenu: {
        color: '#191D1D',
        optionTextColor: '#fff',
        optionCheckColor: '#16987D'
      }
    }
  },
  Card: {
    colorModal: '#191D1D'
  },
  Button: {
    borderFocusPrimary: '1px solid #16987D',
    borderHoverPrimary: '1px solid #16987D',
    borderPrimary: '1px solid #16987D',
    colorPrimary: '#16987D',
    colorFocusPrimary: '#16987D',
    colorHoverPrimary: '#16987D',
    textColorPrimary: '#EBEBEB'
  },
  DataTable: {
    thColorModal: 'unset'
  },
  Table: {
    thColorModal: '#191D1D',
    tdColorModal: '#191D1D'
  },
  Tag: {
    borderPrimary: '1px solid #16987D',
    textColorPrimary: '#16987D'
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
