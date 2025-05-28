import './index.scss';
import dayjs from 'dayjs';
import prettyBytes from 'pretty-bytes';

import { Table, Dropdown, Tooltip, Space, Button, Modal } from 'antd';
import { Copy, Folder, Trash2, PenLine, Download, FileOutput, Ellipsis, File as FileIcon } from 'lucide-react';

import type { TableProps, MenuProps, ModalFuncProps } from 'antd';
import type { FileItem } from '@/types/file.type';

interface FileTableProps {
  fileList: FileItem[];
  compact: boolean;

  onRenameFile: (path: string) => void;
  onOpenFolder: (path: string) => void;
  onDeleteFile?: (path: string) => void;
}

const FileTable: React.FC<FileTableProps> = ({ fileList, compact, onOpenFolder, onDeleteFile, onRenameFile }) => {
  const getMenuItems = (record: FileItem): MenuProps['items'] => [
    {
      label: '复制',
      key: 'copy',
      icon: <Copy size={14} />,
      onClick: () => {
        console.log('复制文件:', record.name);
      }
    },
    {
      label: '移动',
      key: 'move',
      icon: <FileOutput size={14} />,
      onClick: () => {
        console.log('移动文件:', record.name);
      }
    },
    {
      label: '删除',
      key: 'delete',
      danger: true,
      icon: <Trash2 size={14} />,
      onClick: () => {
        const config: ModalFuncProps = {
          title: '确认删除',
          content: `确定要删除${record.is_dir ? '文件夹' : '文件'} "${record.name}" 吗？`,
          okText: '删除',
          cancelText: '取消',
          okButtonProps: { danger: true },
          className: 'dark-theme-modal',
          onOk: () => {
            if (onDeleteFile) {
              onDeleteFile(record.name);
            }
          }
        };

        Modal.confirm(config);
      }
    }
  ];

  const columns: TableProps<FileItem>['columns'] = [
    {
      title: '文件名',
      dataIndex: 'name',
      key: 'name',
      width: 200,
      render: (_, record) => (
        <Tooltip placement="topLeft" mouseEnterDelay={0.5} title={record.name}>
          <div className="flex items-center space-x-2 max-w-40 overflow-hidden">
            <span className="flex-shrink-0">{record.is_dir ? <Folder size={16} /> : <FileIcon size={16} />}</span>
            <span className="truncate">{record.name}</span>
          </div>
        </Tooltip>
      )
    },
    {
      title: '大小',
      dataIndex: 'size',
      key: 'size',
      width: 150,
      render: (_, record) => {
        return prettyBytes(Number(record.size));
      }
    },
    {
      title: '修改时间',
      dataIndex: 'mod_time',
      key: 'mod_time',
      width: 180,
      render: (_, record) => {
        const timestamp = Number(record.mod_time) * 1000;

        return dayjs(timestamp).format('YYYY-MM-DD HH:mm:ss');
      }
    },
    {
      title: '权限',
      key: 'perm',
      width: 100,
      dataIndex: 'perm'
    },
    {
      title: '操作',
      key: 'action',
      width: 200,
      render: (_, record) => {
        return (
          <Space>
            <Tooltip title="下载">
              <Button size="small" icon={<Download size={14} />} />
            </Tooltip>

            <Tooltip title="重命名">
              <Button size="small" icon={<PenLine size={14} />} onClick={() => onRenameFile(record.name)} />
            </Tooltip>

            <Dropdown trigger={['click']} arrow={true} menu={{ items: getMenuItems(record) }}>
              <Button size="small" icon={<Ellipsis size={14} />} />
            </Dropdown>
          </Space>
        );
      }
    }
  ];

  return (
    <Table<FileItem>
      pagination={false}
      columns={columns}
      size={compact ? 'small' : 'middle'}
      dataSource={fileList}
      onRow={record => {
        return {
          onDoubleClick: _ => {
            console.log(record);
            if (record.is_dir) {
              // 进入到文件夹
              onOpenFolder(record.name);
              return;
            }

            // 打来文件编辑器
          }
        };
      }}
    />
  );
};

export default FileTable;
