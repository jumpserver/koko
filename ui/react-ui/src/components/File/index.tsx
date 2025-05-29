import './index.scss';
import FileModal from './widgets/FileModal';
import FileTable from './widgets/FileTable';

import { useEffect, useState } from 'react';
import { FILE_OPERATION_TYPE } from '@/enums';
import { useFileStatus } from '@/store/useFileStatus';
import { useFileConnection } from '@/hooks/useFileConnection';
import { Plus, Upload, RefreshCcw, Undo2 } from 'lucide-react';
import { Card, Flex, Tooltip, Button, Switch, Spin } from 'antd';

import type { FileItem } from '@/types/file.type';

interface CardExtraProps {
  compact: boolean;
  setCompact: (compact: boolean) => void;
}

const CardExtra: React.FC<CardExtraProps> = ({ compact, setCompact }) => {
  return (
    <Tooltip title="紧凑表格">
      <Switch size="small" checked={compact} onChange={setCompact} />
    </Tooltip>
  );
};

const File: React.FC = () => {
  const [compact, setCompact] = useState(false);
  const [fileModalVisible, setFileModalVisible] = useState(false);
  const [fileModalTitle, setFileModalTitle] = useState('');
  const [fileRenamePath, setFileRenamePath] = useState('');
  const [fileModalType, setFileModalType] = useState<'create' | 'rename'>('create');
  const [fileList, setFileList] = useState<FileItem[]>([]);

  const { spinning, createFileSocket, handleFileOperation } = useFileConnection();
  const { loadedMessage, fileMessage, setLoaded } = useFileStatus();

  useEffect(() => {
    if (loadedMessage.token && !loadedMessage.loaded) {
      createFileSocket(loadedMessage.token);
      setLoaded(true);
    }
  }, [loadedMessage.loaded, loadedMessage.token]);

  useEffect(() => {
    setFileList(fileMessage.fileList);
  }, [fileMessage]);

  return (
    <>
      <Card
        title="yy 的文件管理器"
        variant="borderless"
        className="w-full"
        extra={<CardExtra compact={compact} setCompact={setCompact} />}
      >
        <Flex vertical gap="middle">
          <Flex vertical gap="small" align="center" justify="start">
            {/* TODO 文件路径 */}

            <Flex align="center" justify="space-between" gap="small" className="shrink-0 w-full">
              <Flex gap="middle">
                <Button icon={<Upload size={14} />}>上传</Button>
                <Button
                  icon={<Plus size={14} />}
                  onClick={() => {
                    setFileModalTitle('新建文件夹');
                    setFileModalVisible(true);
                  }}
                >
                  新建文件夹
                </Button>
              </Flex>

              <Flex gap="small">
                <Tooltip title="返回到上一层级">
                  <Button
                    icon={<Undo2 size={14} />}
                    onClick={() => handleFileOperation(FILE_OPERATION_TYPE.OPEN_FOLDER)}
                  />
                </Tooltip>

                <Tooltip title="刷新">
                  <Button
                    icon={<RefreshCcw size={14} />}
                    onClick={() => handleFileOperation(FILE_OPERATION_TYPE.REFRESH)}
                  />
                </Tooltip>
              </Flex>
            </Flex>
          </Flex>

          <Spin spinning={spinning} tip="加载中...">
            <FileTable
              fileList={fileList}
              compact={compact}
              onRenameFile={(path: string) => {
                setFileRenamePath(path);
                setFileModalType('rename');
                setFileModalTitle('重命名文件');
                setFileModalVisible(true);
              }}
              onOpenFolder={path => handleFileOperation(FILE_OPERATION_TYPE.OPEN_FOLDER, path)}
              onDeleteFile={path => handleFileOperation(FILE_OPERATION_TYPE.DELETE, path)}
            />
          </Spin>
        </Flex>
      </Card>

      <FileModal
        title={fileModalTitle}
        visible={fileModalVisible}
        onCancel={() => setFileModalVisible(false)}
        onConfirm={fileName => {
          if (fileModalType === 'create') {
            handleFileOperation(FILE_OPERATION_TYPE.CREATE_FOLDER, fileName);
          } else {
            handleFileOperation(FILE_OPERATION_TYPE.RENAME, fileRenamePath, fileName);
          }
          setFileModalVisible(false);
        }}
      />
    </>
  );
};

export default File;
