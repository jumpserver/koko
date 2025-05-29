import FileModal from './widgets/FileModal';
import FileTable from './widgets/FileTable';
import FileUpload from './widgets/FileUpload';
import UploadList from './widgets/UploadList';

import { useEffect, useState } from 'react';
import { FILE_OPERATION_TYPE } from '@/enums';
import { useFileStatus } from '@/store/useFileStatus';
import { Plus, RefreshCcw, Undo2, List } from 'lucide-react';
import { useFileConnection } from '@/hooks/useFileConnection';
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
  const [fileListVisible, setFileListVisible] = useState(false);
  const [fileModalVisible, setFileModalVisible] = useState(false);
  const [fileModalTitle, setFileModalTitle] = useState('');
  const [fileRenamePath, setFileRenamePath] = useState('');
  const [fileModalType, setFileModalType] = useState<'create' | 'rename'>('create');
  const [fileList, setFileList] = useState<FileItem[]>([]);

  const {
    spinning,
    currentUploadMessage,
    createFileSocket,
    handleFileOperation,
    handleFileUpload,
  } = useFileConnection();
  const { loadedMessage, fileMessage, uploadFileList, setLoaded } = useFileStatus();

  useEffect(() => {
    if (loadedMessage.token && !loadedMessage.loaded) {
      createFileSocket(loadedMessage.token);
      setLoaded(true);
    }
  }, [loadedMessage.loaded, loadedMessage.token]);

  useEffect(() => {
    setFileList(fileMessage.fileList);
  }, [fileMessage]);

  useEffect(() => {
    if (currentUploadMessage?.status === 'uploading') {
      setFileListVisible(true);
    }
  }, [currentUploadMessage]);

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
                <FileUpload handleFileUpload={handleFileUpload} />

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
                {/* TODO 根路径下的禁用 */}
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

                <Tooltip title="上传列表">
                  <Button icon={<List size={14} />} onClick={() => setFileListVisible(true)} />
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

      <UploadList
        uploadFileList={uploadFileList}
        fileListVisible={fileListVisible}
        currentUploadMessage={currentUploadMessage!}
        closeFileList={() => setFileListVisible(false)}
      />
    </>
  );
};

export default File;
