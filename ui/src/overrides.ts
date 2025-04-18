import type { GlobalThemeOverrides } from 'naive-ui';

export const themeOverrides: GlobalThemeOverrides = {
  Drawer: {
    color: '#202222',
    titleTextColor: '#EBEBEB'
  },
  Form: {},
  Select: {
    peers: {
      InternalSelection: {
        color: '#202222',
        border: '1px solid #FFFFFF17',
        borderHover: '1px solid #16987D',
        borderActive: '1px solid #16987D',
        borderFocus: '1px solid #16987D'
      },
      InternalSelectMenu: {
        color: '#303336',
        optionTextColor: '#fff',
        optionCheckColor: '#16987D'
      }
    }
  },
  Card: {
    colorModal: '#202222'
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
    thColorModal: '#202222',
    tdColorModal: '#202222',
    tdColorHoverModal: 'rgba(255, 255, 255, 0.08)',
    borderColorModal: 'rgba(255, 255, 255, 0.08)',
    borderColorHoverModal: 'rgba(255, 255, 255, 0.08)'
  },
  Table: {
    thColorModal: '#202222',
    tdColorModal: '#202222'
  },
  Tag: {
    borderPrimary: '1px solid #16987D',
    textColorPrimary: '#16987D'
  },
  Upload: {
    peers: {
      Progress: {
        fillColor: '#16987D',
        fillColorInfo: '#16987D'
      }
    }
  }
};
