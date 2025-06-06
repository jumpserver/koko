import { NIcon, NText } from 'naive-ui';
import { Download } from '@vicons/tabler';
import { Delete16Regular } from '@vicons/fluent';
import { DriveFileRenameOutlineOutlined } from '@vicons/material';

import { h } from 'vue';

import type { Component } from 'vue';
import type { DropdownOption } from 'naive-ui';

const renderIcon = (icon: Component) => {
  return () => {
    return h(
      NIcon,
      { size: 20 },
      {
        default: () => h(icon)
      }
    );
  };
};

export const getDropSelections = (t: any): DropdownOption[] => {
  return [
    {
      key: 'rename',
      label: t('Rename'),
      icon: renderIcon(DriveFileRenameOutlineOutlined)
    },
    {
      key: 'download',
      label: t('Download'),
      icon: renderIcon(Download)
    },
    {
      type: 'divider',
      key: 'd1'
    },
    {
      key: 'delete',
      icon: () => {
        return <NIcon size={20} color="#F54A45" component={Delete16Regular} />;
      },
      label: () => {
        return (
          <NText depth={1} style={{ color: '#F54A45' }}>
            {t('Delete')}
          </NText>
        );
      }
    }
  ];
};
