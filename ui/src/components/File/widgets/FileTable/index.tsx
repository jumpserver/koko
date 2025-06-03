import './index.scss';
import dayjs from 'dayjs';
import prettyBytes from 'pretty-bytes';
import Highlighter from 'react-highlight-words';

import { useState, useRef } from 'react';
import { SearchOutlined } from '@ant-design/icons';
import { Table, Dropdown, Tooltip, Space, Button, Modal, Input } from 'antd';
import { Copy, Folder, Trash2, PenLine, Download, FileOutput, Ellipsis, File as FileIcon } from 'lucide-react';

import type { FileItem } from '@/types/file.type';
import type { TableProps, MenuProps, ModalFuncProps, TableColumnType, InputRef } from 'antd';
import type { FilterDropdownProps } from 'antd/es/table/interface';

interface FileTableProps {
  fileList: FileItem[];
  compact: boolean;

  onRenameFile: (path: string) => void;
  onOpenFolder: (path: string) => void;
  onDeleteFile: (path: string) => void;
  onDownloadFile: (path: string, is_dir: boolean) => void;
}

const FileTable: React.FC<FileTableProps> = ({
  fileList,
  compact,
  onOpenFolder,
  onDeleteFile,
  onRenameFile,
  onDownloadFile
}) => {
  const [searchText, setSearchText] = useState('');
  const [searchedColumn, setSearchedColumn] = useState('');
  const searchInput = useRef<InputRef>(null);

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
            onDeleteFile(record.name);
          }
        };

        Modal.confirm(config);
      }
    }
  ];

  const handleSearch = (selectedKeys: string[], confirm: FilterDropdownProps['confirm'], dataIndex: keyof FileItem) => {
    confirm();
    setSearchText(selectedKeys[0]);
    setSearchedColumn(dataIndex);
  };

  const handleReset = (clearFilters: () => void) => {
    clearFilters();
    setSearchText('');
  };

  const getColumnSearchProps = (dataIndex: keyof FileItem): TableColumnType<FileItem> => {
    return {
      filterDropdown: ({ setSelectedKeys, selectedKeys, confirm, clearFilters }) => (
        <div style={{ padding: 8 }} onKeyDown={e => e.stopPropagation()}>
          <Input
            ref={searchInput}
            placeholder={`Search ${dataIndex}`}
            value={selectedKeys[0]}
            onChange={e => setSelectedKeys(e.target.value ? [e.target.value] : [])}
            onPressEnter={() => handleSearch(selectedKeys as string[], confirm, dataIndex)}
            style={{ marginBottom: 8, display: 'block' }}
          />
          <Space>
            <Button
              type="primary"
              onClick={() => handleSearch(selectedKeys as string[], confirm, dataIndex)}
              icon={<SearchOutlined />}
              size="small"
            >
              搜索
            </Button>
            <Button onClick={() => clearFilters && handleReset(clearFilters)} size="small">
              重置
            </Button>
            <Button
              type="link"
              size="small"
              onClick={() => {
                confirm({ closeDropdown: false });
                setSearchText((selectedKeys as string[])[0]);
                setSearchedColumn(dataIndex);
              }}
            >
              过滤
            </Button>
          </Space>
        </div>
      ),
      filterIcon: (filtered: boolean) => <SearchOutlined style={{ color: filtered ? '#1677ff' : undefined }} />,
      onFilter: (value, record) =>
        record[dataIndex]
          ?.toString()
          .toLowerCase()
          .includes((value as string).toLowerCase()) || false,
      filterDropdownProps: {
        onOpenChange(open) {
          if (open) {
            setTimeout(() => searchInput.current?.select(), 100);
          }
        }
      }
    };
  };

  const columns: TableProps<FileItem>['columns'] = [
    {
      title: '文件名',
      dataIndex: 'name',
      key: 'name',
      width: 200,
      sorter: (a, b) => a.name.localeCompare(b.name),
      ...getColumnSearchProps('name'),
      render: (text, record) => {
        const displayText =
          searchedColumn === 'name' ? (
            <Highlighter
              highlightStyle={{ backgroundColor: '#ffc069', padding: 0 }}
              searchWords={[searchText]}
              autoEscape
              textToHighlight={text ? text.toString() : ''}
            />
          ) : (
            text
          );

        return (
          <Tooltip placement="topLeft" mouseEnterDelay={0.5} title={record.name}>
            <div className="flex items-center space-x-2 max-w-40 overflow-hidden">
              <span className="flex-shrink-0">{record.is_dir ? <Folder size={16} /> : <FileIcon size={16} />}</span>
              <span className="truncate">{displayText}</span>
            </div>
          </Tooltip>
        );
      }
    },
    {
      title: '大小',
      dataIndex: 'size',
      key: 'size',
      width: 100,
      render: (_, record) => {
        return prettyBytes(Number(record.size));
      }
    },
    {
      title: '修改时间',
      dataIndex: 'mod_time',
      key: 'mod_time',
      width: 200,
      render: (_, record) => {
        const timestamp = Number(record.mod_time) * 1000;

        return dayjs(timestamp).format('YYYY-MM-DD HH:mm:ss');
      }
    },
    {
      title: '权限',
      key: 'perm',
      width: 150,
      dataIndex: 'perm'
    },
    {
      title: '操作',
      key: 'action',
      width: 120,
      render: (_, record) => {
        return (
          <Space>
            <Tooltip title="下载">
              <Button
                size="small"
                icon={<Download size={14} />}
                onClick={() => onDownloadFile(record.name, record.is_dir)}
              />
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
