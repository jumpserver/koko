import { Download } from '@vicons/tabler';
import { NIcon, NButton, NText } from 'naive-ui';
import { Delete16Regular } from '@vicons/fluent';
import { DriveFileRenameOutlineOutlined } from '@vicons/material';

import { h } from 'vue';

import type { Component } from 'vue';
import type { DropdownOption } from 'naive-ui';

const renderIcon = (icon: Component) => {
  return () => {
    return h(NIcon, null, {
      default: () => h(icon)
    });
  };
};

export const getDropSelections = (t: any): DropdownOption[] => {
  return [
    {
      key: 'rename',
      label: t('rename'),
      icon: renderIcon(DriveFileRenameOutlineOutlined)
    },
    {
      key: 'download',
      label: t('download'),
      icon: renderIcon(Download)
    },
    {
      type: 'divider',
      key: 'd1'
    },
    {
      key: 'delete',
      icon: () =>
        h(
          NIcon,
          {
            color: 'red'
          },
          {
            default: () => h(Delete16Regular)
          }
        ),
      label: () =>
        h(
          NText,
          {
            depth: 1,
            style: {
              color: 'red'
            }
          },
          {
            default: () => t('delete')
          }
        )
    }
  ];
};
