import type { GlobalThemeOverrides } from 'naive-ui';

import { useColor } from './hooks/useColor';

const { darken, lighten, alpha, setCurrentMainColor } = useColor();

// 创建主题生成函数
export const createThemeOverrides = (
  themeType: 'default' | 'deepBlue' | 'darkGary' = 'default'
): GlobalThemeOverrides => {
  setCurrentMainColor(themeType);

  const primaryColor = lighten(0);
  const primaryColorHover = lighten(10);
  const primaryColorPressed = darken(10);
  const backgroundColor = darken(20);
  const surfaceColor = lighten(5);
  const borderColor = alpha(0.09);
  const textColor = 'rgba(235, 235, 235, 1)';
  const textColorSecondary = alpha(0.8, '#FFFFFF');
  const hoverColor = alpha(0.08, '#FFFFFF');

  return {
    Drawer: {
      color: backgroundColor,
      titleTextColor: textColor,
    },
    Tabs: {
      tabPaddingVerticalSmallLine: '6px 12px 6px 0',
    },
    Form: {},
    Tree: {
      nodeColorActive: alpha(0.1),
    },
    Input: {
      color: backgroundColor,
      border: `1px solid ${borderColor}`,
      borderHover: `1px solid ${primaryColor}`,
      borderActive: `1px solid ${primaryColor}`,
      borderFocus: `1px solid ${primaryColor}`,
    },
    List: {
      colorModal: backgroundColor,
    },
    Select: {
      peers: {
        InternalSelection: {
          color: backgroundColor,
          border: `1px solid ${borderColor}`,
          borderHover: `1px solid ${primaryColor}`,
          borderActive: `1px solid ${primaryColor}`,
          borderFocus: `1px solid ${primaryColor}`,
        },
        InternalSelectMenu: {
          color: surfaceColor,
          optionTextColor: textColor,
          optionCheckColor: primaryColor,
        },
      },
    },
    Modal: {
      peers: {
        Dialog: {
          color: backgroundColor,
          peers: {
            Button: {
              borderPressedPrimary: `1px solid ${primaryColorPressed}`,
              borderFocusPrimary: `1px solid ${primaryColor}`,
              borderHoverPrimary: `1px solid ${primaryColorHover}`,
              borderPrimary: `1px solid ${primaryColor}`,
              colorPrimary: primaryColor,
              colorFocusPrimary: primaryColor,
              colorHoverPrimary: primaryColorHover,
              colorPressedPrimary: primaryColorPressed,
              textColorPrimary: textColor,
              textColorHoverPrimary: textColor,
              textColorPressedPrimary: textColor,
              textColorFocusPrimary: textColor,
              textColorError: textColor,
              textColorHoverError: textColor,
              textColorPressedError: textColor,
              textColorFocusError: textColor,
            },
          },
        },
      },
    },
    Card: {
      color: backgroundColor,
      colorModal: backgroundColor,
    },
    Button: {
      borderPressedPrimary: `1px solid ${primaryColorPressed}`,
      borderFocusPrimary: `1px solid ${primaryColor}`,
      borderHoverPrimary: `1px solid ${primaryColorHover}`,
      borderPrimary: `1px solid ${primaryColor}`,
      colorPrimary: primaryColor,
      colorFocusPrimary: primaryColor,
      colorHoverPrimary: primaryColorHover,
      colorPressedPrimary: primaryColorPressed,
      textColorPrimary: textColor,
      textColorHoverPrimary: textColor,
      textColorPressedPrimary: textColor,
      textColorFocusPrimary: textColor,
    },
    Switch: {
      railColorActive: textColorSecondary,
      buttonColor: backgroundColor,
    },
    DataTable: {
      thColor: backgroundColor,
      tdColor: backgroundColor,
      tdColorHover: hoverColor,
      thColorModal: backgroundColor,
      tdColorModal: backgroundColor,
      tdColorHoverModal: hoverColor,
      borderColorModal: borderColor,
      borderColorHoverModal: borderColor,
    },
    Ellipsis: {
      textColor,
      peers: {
        Tooltip: {
          color: surfaceColor,
          textColor,
          peers: {
            Popover: {
              color: surfaceColor,
              textColor,
            },
          },
        },
      },
    },
    Table: {
      thColorModal: backgroundColor,
      tdColorModal: backgroundColor,
    },
    Tag: {
      borderPrimary: `1px solid ${primaryColor}`,
      textColorPrimary: primaryColor,
    },
    Upload: {
      peers: {
        Progress: {
          fillColor: primaryColor,
          fillColorInfo: primaryColor,
        },
      },
    },
    Layout: {
      color: darken(40),
      siderColor: darken(40),
      headerColor: darken(40),
    },
  };
};
