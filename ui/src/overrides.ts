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
  const backgroundColor = darken(5);
  const cardBackgroundColor = darken(3);
  const inputBackgroundColor = lighten(2);
  const surfaceColor = lighten(8);
  const borderColor = alpha(0.3);
  const textColor = 'rgba(235, 235, 235, 1)';
  const textColorSecondary = alpha(0.8, '#FFFFFF');
  const hoverColor = alpha(0.12, '#FFFFFF');

  return {
    Tabs: {
      tabPaddingVerticalSmallLine: '6px 12px 6px 0',
    },
    Form: {
      labelTextColor: textColor,
      labelTextColorDisabled: textColorSecondary,
      asteriskColor: primaryColor,
      feedbackTextColor: textColor,
      feedbackTextColorError: '#ff6b6b',
      feedbackTextColorWarning: '#FFB020',
      feedbackTextColorSuccess: lighten(8),
      feedbackPadding: '4px 0 0 0',
    },
    Tree: {
      nodeColorActive: alpha(0.1),
    },
    Input: {
      color: inputBackgroundColor,
      colorFocus: inputBackgroundColor,
      colorDisabled: darken(10),
      border: `1px solid ${borderColor}`,
      borderHover: `1px solid ${primaryColor}`,
      borderActive: `1px solid ${primaryColor}`,
      borderFocus: `1px solid ${primaryColor}`,
      borderDisabled: `1px solid ${alpha(0.1)}`,
      textColor,
      textColorDisabled: textColorSecondary,
      placeholderColor: textColorSecondary,
      placeholderColorDisabled: alpha(0.4),
      caretColor: '#FFFFFF',
      boxShadowFocus: `0 0 0 2px ${alpha(0.2)}`,
    },
    List: {
      colorHover: backgroundColor,
      colorModal: backgroundColor,
      colorHoverModal: hoverColor,
      borderColor,
      peers: {
        ListItem: {
          colorHover: hoverColor,
          colorHoverModal: hoverColor,
          borderRadius: '6px',
        },
      },
    },
    Select: {
      peers: {
        InternalSelection: {
          color: backgroundColor,
          colorDisabled: darken(10),
          border: `1px solid ${borderColor}`,
          borderHover: `1px solid ${primaryColor}`,
          borderActive: `1px solid ${primaryColor}`,
          borderFocus: `1px solid ${primaryColor}`,
          textColor,
          textColorDisabled: textColorSecondary,
          placeholderColor: textColorSecondary,
          placeholderColorDisabled: alpha(0.4),
          boxShadowFocus: `0 0 0 2px ${alpha(0.2)}`,
        },
        InternalSelectMenu: {
          color: surfaceColor,
          optionTextColor: textColor,
          optionTextColorActive: textColor,
          optionTextColorPressed: textColor,
          optionCheckColor: primaryColor,
          groupHeaderTextColor: textColorSecondary,
          actionTextColor: textColorSecondary,
          loadingColor: primaryColor,
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
      color: cardBackgroundColor,
      colorModal: cardBackgroundColor,
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
      railColor: alpha(0.3),
      railColorActive: primaryColor,
      buttonColor: backgroundColor,
      buttonColorPressed: darken(5),
      textColor,
      textColorDisabled: textColorSecondary,
      opacityDisabled: 0.4,
    },
    Checkbox: {
      color: backgroundColor,
      colorChecked: primaryColor,
      colorDisabled: darken(10),
      colorDisabledChecked: alpha(0.3),
      textColor,
      textColorDisabled: textColorSecondary,
      dotColorDisabled: alpha(0.6),
      checkMarkColor: textColor,
      checkMarkColorDisabled: textColorSecondary,
      border: `1px solid ${borderColor}`,
      borderFocus: `1px solid ${primaryColor}`,
      borderDisabled: `1px solid ${alpha(0.1)}`,
      borderChecked: `1px solid ${primaryColor}`,
      boxShadowFocus: `0 0 0 2px ${alpha(0.2)}`,
    },
    Radio: {
      color: backgroundColor,
      colorDisabled: darken(10),
      colorChecked: primaryColor,
      dotColorActive: textColor,
      dotColorDisabled: textColorSecondary,
      textColor,
      textColorDisabled: textColorSecondary,
      buttonBorderColor: borderColor,
      buttonBorderColorActive: primaryColor,
      buttonColorActive: primaryColor,
      buttonTextColor: textColor,
      buttonTextColorActive: textColor,
      buttonTextColorHover: textColor,
      buttonColorHover: lighten(10),
      buttonColorPressed: darken(10),
      boxShadowFocus: `0 0 0 2px ${alpha(0.2)}`,
    },
    DataTable: {
      thColor: cardBackgroundColor,
      tdColor: cardBackgroundColor,
      tdColorHover: hoverColor,
      thColorModal: cardBackgroundColor,
      tdColorModal: cardBackgroundColor,
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
      thColorModal: cardBackgroundColor,
      tdColorModal: cardBackgroundColor,
    },
    Tag: {
      borderPrimary: `1px solid ${primaryColor}`,
      textColorPrimary: textColor,
      colorSuccess: lighten(5),
      borderSuccess: `1px solid ${lighten(8)}`,
      textColorSuccess: textColor,
      closeColorSuccess: textColorSecondary,
      closeColorHoverSuccess: textColor,
      closeColorPressedSuccess: darken(5),
      colorWarning: alpha(0.1, '#FFB020'),
      borderWarning: `1px solid ${alpha(0.3, '#FFB020')}`,
      textColorWarning: '#FFB020',
      closeColorWarning: alpha(0.6, '#FFB020'),
      closeColorHoverWarning: '#FFB020',
      closeColorPressedWarning: alpha(0.8, '#FFB020'),
      color: cardBackgroundColor,
      textColor: textColorSecondary,
      border: `1px solid ${borderColor}`,
      closeColor: textColorSecondary,
      closeColorHover: textColor,
      closeColorPressed: darken(5),
      closeIconColor: alpha(0.8, '#FF0000'),
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
    Drawer: {
      color: backgroundColor,
      titleTextColor: textColor,
      bodyPadding: '16px 24px',
    },
    Dropdown: {
      color: surfaceColor,
      optionTextColor: textColor,
      optionTextColorHover: textColor,
      optionTextColorActive: textColor,
      optionTextColorChildActive: primaryColor,
      optionColorHover: hoverColor,
      optionColorActive: alpha(0.15),
      optionColorPressed: alpha(0.2),
      groupHeaderTextColor: textColorSecondary,
      dividerColor: alpha(0.3),
      optionOpacityDisabled: 0.4,
      optionCheckColor: primaryColor,
      optionArrowColor: textColorSecondary,
      borderColor,
    },
    Popover: {
      color: surfaceColor,
      textColor,
      borderColor,
      borderRadius: '8px',
      boxShadow: '0 4px 12px rgba(0, 0, 0, 0.15)',
      arrowColor: surfaceColor,
      arrowColorInfo: surfaceColor,
      arrowColorSuccess: lighten(5),
      arrowColorWarning: alpha(0.1, '#FFB020'),
      arrowColorError: alpha(0.1, '#ff6b6b'),
      colorInfo: surfaceColor,
      colorSuccess: lighten(5),
      colorWarning: alpha(0.1, '#FFB020'),
      colorError: alpha(0.1, '#ff6b6b'),
      textColorInfo: textColor,
      textColorSuccess: textColor,
      textColorWarning: '#FFB020',
      textColorError: '#ff6b6b',
      borderColorInfo: borderColor,
      borderColorSuccess: alpha(0.3, lighten(8)),
      borderColorWarning: alpha(0.3, '#FFB020'),
      borderColorError: alpha(0.3, '#ff6b6b'),
    },
    Popconfirm: {
      peers: {
        Popover: {
          color: surfaceColor,
          textColor,
        },
        Button: {
          colorPrimary: primaryColor,
          colorHoverPrimary: primaryColorHover,
          colorPressedPrimary: primaryColorPressed,
          textColorPrimary: textColor,
          textColorHoverPrimary: textColor,
          textColorPressedPrimary: textColor,

          color: cardBackgroundColor,
          colorHover: hoverColor,
          colorPressed: alpha(0.2),
          textColor,
          textColorHover: textColor,
          textColorPressed: textColor,
        },
      },
    },
    Descriptions: {
      titleTextColor: textColor,
      thColor: cardBackgroundColor,
      thColorModal: cardBackgroundColor,
      thColorPopover: cardBackgroundColor,
      thTextColor: textColor,
      tdTextColor: textColor,
      tdColor: backgroundColor,
      tdColorModal: backgroundColor,
      tdColorPopover: backgroundColor,
      borderColorModal: lighten(10),
    },
  };
};
