import dayjs from 'dayjs';
import prettyBytes from 'pretty-bytes';

import { useEffect, useState, useRef } from 'react';
import { useFileStatus } from '@/store/useFileStatus';
import { useFileConnection } from '@/hooks/useFileConnection';
import { Download, Plus, Folder, File as FileIcon, RefreshCcw } from 'lucide-react';
import { Card, Flex, Table, Tooltip, Space, Button, Dropdown, theme } from 'antd';

import type { TableProps, MenuProps } from 'antd';
import type { FileItem } from '@/types/file.type';

const columns: TableProps<FileItem>['columns'] = [
  {
    title: '文件名',
    dataIndex: 'name',
    key: 'name',
    width: 200,

    render: (_, { name, is_dir }) => (
      <Tooltip placement="topLeft" mouseEnterDelay={0.5} title={name}>
        <div className="flex items-center space-x-2 max-w-40 overflow-hidden">
          <span className="flex-shrink-0">{is_dir ? <Folder size={16} /> : <FileIcon size={16} />}</span>
          <span className="truncate">{name}</span>
        </div>
      </Tooltip>
    )
  },
  {
    title: '大小',
    dataIndex: 'size',
    key: 'size',
    render: (_, { size }) => {
      return prettyBytes(Number(size));
    }
  },
  {
    title: '修改时间',
    dataIndex: 'mod_time',
    key: 'mod_time',
    width: 180,
    render: (_, { mod_time }) => {
      const timestamp = Number(mod_time) * 1000;

      return dayjs(timestamp).format('YYYY-MM-DD HH:mm:ss');
    }
  },
  {
    title: '权限',
    key: 'perm',
    dataIndex: 'perm'
  }
];

const items: MenuProps['items'] = [
  {
    label: '1st menu item',
    key: '1',
    onClick: e => {
      console.log(e);
    }
  },
  {
    label: '2nd menu item',
    key: '2'
  },
  {
    label: '3rd menu item',
    key: '3'
  }
];

const File: React.FC = () => {
  const { createFileSocket, handleRefresh } = useFileConnection();
  const { loadedMessage, fileMessage, setLoaded } = useFileStatus();

  const [selectedRow, setSelectedRow] = useState<FileItem | null>(null);
  const [visible, setVisible] = useState(false);
  const [position, setPosition] = useState({ x: 0, y: 0 });
  const [fileList, setFileList] = useState<FileItem[]>([]);

  useEffect(() => {
    if (!loadedMessage.loaded) {
      createFileSocket(loadedMessage.token);
      setLoaded(true);
    }
  }, [loadedMessage.loaded, loadedMessage.token]);

  useEffect(() => {
    console.log(fileMessage);
    const { fileList } = fileMessage;

    if (fileList.length > 0) {
      setFileList(fileList);
    }
  }, [fileMessage]);

  // 动态生成菜单项
  const getMenuItems = (record: FileItem) => [
    {
      label: `下载 ${record.name}`,
      key: 'download',
      onClick: () => {
        console.log('下载文件:', record);
        // 实现下载逻辑
      }
    },
    {
      label: record.is_dir ? '打开文件夹' : '打开文件',
      key: 'open',
      onClick: () => {
        console.log('打开:', record);
        // 实现打开逻辑
      }
    },
    {
      label: '删除',
      key: 'delete',
      danger: true,
      onClick: () => {
        console.log('删除:', record);
        // 实现删除逻辑
      }
    }
  ];

  return (
    <Card title="yy 的文件管理器" variant="borderless" className="w-full">
      <Flex vertical gap="middle">
        <Flex align="center" className="w-full">
          <div className="flex-1">
            <Space split=">">
              {fileMessage.paths.map((item, index) => (
                <span key={index}>{item}</span>
              ))}
            </Space>
          </div>

          <Flex align="center" gap="small" className="shrink-0">
            <Button icon={<Download size={14} />}>下载</Button>
            <Button icon={<Plus size={14} />}>新建文件夹</Button>

            <Tooltip title="刷新">
              <Button icon={<RefreshCcw size={14} />} onClick={handleRefresh} />
            </Tooltip>
          </Flex>
        </Flex>

        <Table<FileItem>
          pagination={false}
          columns={columns}
          dataSource={fileList}
          onRow={record => ({
            onContextMenu: event => {
              event.preventDefault();
              setPosition({ x: event.clientX, y: event.clientY });
              setSelectedRow(record);
              setVisible(true);
            }
          })}
        />

        {selectedRow && (
          <Dropdown
            open={visible}
            menu={{ items: getMenuItems(selectedRow) }}
            onOpenChange={open => {
              if (!open) {
                setVisible(false);
                setSelectedRow(null);
              }
            }}
            dropdownRender={menu => (
              <div
                style={{
                  position: 'fixed',
                  left: `${position.x}px`,
                  top: `${position.y}px`
                }}
              >
                {menu}
              </div>
            )}
          >
            <span></span>
          </Dropdown>
        )}
      </Flex>
    </Card>
  );
};

export default File;
