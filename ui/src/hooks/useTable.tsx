import dayjs from 'dayjs';
import { reactive } from 'vue';
import { NText } from 'naive-ui';
import { useI18n } from 'vue-i18n';
import { getFileName } from '@/utils';

import type { DataTableColumns, TreeOption } from 'naive-ui';
import type { RowData } from '@/types/modules/table.type';

export const useTable = () => {
  /**
   * @description 处理 size
   */
  const formatBytes = (bytes: string | number, decimals: number = 2): string => {
    const byteNumber = typeof bytes === 'string' ? parseInt(bytes, 10) : Number(bytes);

    if (isNaN(byteNumber) || byteNumber <= 0) return '0 Byte';

    const units = ['Byte', 'KB', 'MB', 'GB', 'TB', 'PB'];

    const i = Math.floor(Math.log2(byteNumber) / Math.log2(1024));

    return (byteNumber / Math.pow(1024, i)).toFixed(decimals) + ' ' + units[Math.min(i, units.length - 1)];
  };

  const createColumns = (): DataTableColumns<RowData> => {
    const { t } = useI18n();

    return [
      {
        title: t('Name'),
        key: 'name',
        ellipsis: {
          tooltip: true
        }
      },
      {
        title: t('LastModified'),
        key: 'mod_time',
        align: 'center',
        ellipsis: {
          tooltip: true
        },
        render: (row: RowData) => {
          return (
            <NText depth={1}>
              {row.mod_time ? dayjs(Number(row.mod_time) * 1000).format('YYYY-MM-DD HH:mm:ss') : '-'}
            </NText>
          );
        }
      },
      {
        title: t('ActionPerm'),
        key: 'perm',
        align: 'center'
      },
      {
        title: t('Size'),
        key: 'size',
        align: 'center',
        render: (row: RowData) => {
          return (
            <NText depth={1} strong={true}>
              {formatBytes(row.size)}
            </NText>
          );
        }
      },
      {
        title: t('Type'),
        key: 'type',
        align: 'center',
        render: (row: RowData) => {
          return (
            <NText depth={1} strong={true}>
              {getFileName(row)}
            </NText>
          );
        }
      }
    ];
  };

  return {
    createColumns
  };
};
