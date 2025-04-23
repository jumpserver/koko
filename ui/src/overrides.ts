import type { GlobalThemeOverrides } from 'naive-ui';

export const themeOverrides: GlobalThemeOverrides = {
  Drawer: {
    color: 'rgba(32, 34, 34, 1)',
    titleTextColor: 'rgba(235, 235, 235, 1)'
  },
  Form: {},
  Tree: {
    nodeColorActive: 'rgba(26, 179, 148, 0.1)'
  },
  Input: {
    color: 'rgba(32, 34, 34, 1)',
    border: '1px solid rgba(255, 255, 255, 0.09)',
    borderHover: '1px solid rgba(22, 152, 125, 1)',
    borderActive: '1px solid rgba(22, 152, 125, 1)',
    borderFocus: '1px solid rgba(22, 152, 125, 1)'
  },
  List: {
    colorModal: 'rgba(32, 34, 34, 1)'
  },
  Select: {
    peers: {
      InternalSelection: {
        color: 'rgba(32, 34, 34, 1)',
        border: '1px solid rgba(255, 255, 255, 0.09)',
        borderHover: '1px solid rgba(22, 152, 125, 1)',
        borderActive: '1px solid rgba(22, 152, 125, 1)',
        borderFocus: '1px solid rgba(22, 152, 125, 1)'
      },
      InternalSelectMenu: {
        color: 'rgba(48, 51, 54, 1)',
        optionTextColor: 'rgba(255, 255, 255, 1)',
        optionCheckColor: 'rgba(22, 152, 125, 1)'
      }
    }
  },
  Modal: {
    peers: {
      Dialog: {
        color: 'rgba(32, 34, 34, 1)',
        peers: {
          Button: {
            borderPressedPrimary: '1px solid rgba(22, 152, 125, 1)',
            borderFocusPrimary: '1px solid rgba(22, 152, 125, 1)',
            borderHoverPrimary: '1px solid rgba(22, 152, 125, 1)',
            borderPrimary: '1px solid rgba(22, 152, 125, 1)',
            colorPrimary: 'rgba(22, 152, 125, 1)',
            colorFocusPrimary: 'rgba(22, 152, 125, 1)',
            colorHoverPrimary: 'rgba(22, 152, 125, 1)',
            colorPressedPrimary: 'rgba(22, 152, 125, 1)',
            textColorPrimary: 'rgba(235, 235, 235, 1)',
            textColorHoverPrimary: 'rgba(235, 235, 235, 1)',
            textColorPressedPrimary: 'rgba(235, 235, 235, 1)',
            textColorFocusPrimary: 'rgba(235, 235, 235, 1)'
          }
        }
      }
    }
  },
  Card: {
    color: 'rgba(32, 34, 34, 1)',
    colorModal: 'rgba(32, 34, 34, 1)'
  },
  Button: {
    borderFocusPrimary: '1px solid rgba(22, 152, 125, 1)',
    borderHoverPrimary: '1px solid rgba(22, 152, 125, 1)',
    borderPrimary: '1px solid rgba(22, 152, 125, 1)',
    colorPrimary: 'rgba(22, 152, 125, 1)',
    colorFocusPrimary: 'rgba(22, 152, 125, 1)',
    colorHoverPrimary: 'rgba(22, 152, 125, 1)',
    textColorPrimary: 'rgba(235, 235, 235, 1)'
  },
  Switch: {
    railColorActive: 'rgba(255, 255, 255, 0.2)',
    buttonColor: 'rgba(32, 34, 34, 1)'
  },

  DataTable: {
    thColor: 'rgba(32, 34, 34, 1)',
    tdColor: 'rgba(32, 34, 34, 1)',
    tdColorHover: 'rgba(255, 255, 255, 0.08)',
    thColorModal: 'rgba(32, 34, 34, 1)',
    tdColorModal: 'rgba(32, 34, 34, 1)',
    tdColorHoverModal: 'rgba(255, 255, 255, 0.08)',
    borderColorModal: 'rgba(255, 255, 255, 0.08)',
    borderColorHoverModal: 'rgba(255, 255, 255, 0.08)'
  },
  Ellipsis: {
    textColor: 'rgba(235, 235, 235, 1)',
    peers: {
      Tooltip: {
        color: 'rgba(48, 51, 54, 1)',
        textColor: 'rgba(255, 255, 255, 1)',
        peers: {
          Popover: {
            color: 'rgba(48, 51, 54, 1)',
            textColor: 'rgba(255, 255, 255, 1)'
          }
        }
      }
    }
  },
  Table: {
    thColorModal: 'rgba(32, 34, 34, 1)',
    tdColorModal: 'rgba(32, 34, 34, 1)'
  },
  Tag: {
    borderPrimary: '1px solid rgba(22, 152, 125, 1)',
    textColorPrimary: 'rgba(22, 152, 125, 1)'
  },
  Upload: {
    peers: {
      Progress: {
        fillColor: 'rgba(22, 152, 125, 1)',
        fillColorInfo: 'rgba(22, 152, 125, 1)'
      }
    }
  }
};
