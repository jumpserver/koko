import FileTable from './widgets/FileTable';
import FileUpload from './widgets/FileUpload';
import UploadList from './widgets/UploadList';

import { useEffect, useState } from 'react';
import { FILE_OPERATION_TYPE } from '@/enums';
import { useFileStatus } from '@/store/useFileStatus';
import { Plus, RefreshCcw, Undo2, List } from 'lucide-react';
import { useFileConnection } from '@/hooks/useFileConnection';
import { Card, Flex, Tooltip, Button, Switch, Spin, Modal, Input, message } from 'antd';

import type { FileItem } from '@/types/file.type';

interface CardExtraProps {
  compact: boolean;
  setCompact: (compact: boolean) => void;
}

type ModalType = 'create' | 'rename';

const CardExtra: React.FC<CardExtraProps> = ({ compact, setCompact }) => {
  return (
    <Tooltip title="紧凑表格">
      <Switch size="small" checked={compact} onChange={setCompact} />
    </Tooltip>
  );
};

const File: React.FC = () => {
  const [modal, contextHolder] = Modal.useModal();
  const [compact, setCompact] = useState(false);
  const [fileListVisible, setFileListVisible] = useState(false);
  const [userClosedUploadList, setUserClosedUploadList] = useState(false);
  const [fileList, setFileList] = useState<FileItem[]>([]);

  const { spinning, currentUploadMessage, createFileSocket, handleFileOperation, handleFileUpload } =
    useFileConnection();
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

  useEffect(() => {
    if (currentUploadMessage?.status === 'uploading' && !userClosedUploadList) {
      setFileListVisible(true);
    }
  }, [currentUploadMessage, userClosedUploadList]);

  const InputComponent = ({
    inputValue,
    onValueChange
  }: {
    inputValue: string;
    onValueChange: (value: string) => void;
  }) => {
    const [value, setValue] = useState(inputValue);

    useEffect(() => {
      onValueChange(value);
    }, [value, onValueChange]);

    return <Input allowClear value={value} onChange={e => setValue(e.target.value)} autoFocus />;
  };

  const createInputModal = (title: string, inputValue: string, type: ModalType, renamePath?: string) => {
    let currentValue = inputValue;

    modal.confirm({
      title,
      icon: null,
      centered: true,
      okText: '确认',
      cancelText: '取消',
      onOk: () => {
        if (!currentValue.trim()) {
          message.error('文件名不能为空');
          return Promise.reject();
        }

        const isExist = fileMessage.fileList.find(item => item.name === currentValue.trim());

        if (isExist && currentValue.trim() !== inputValue) {
          message.error('文件名已存在');
          return Promise.reject();
        }

        if (type === 'create') {
          handleFileOperation(FILE_OPERATION_TYPE.CREATE_FOLDER, currentValue.trim());
        } else {
          handleFileOperation(FILE_OPERATION_TYPE.RENAME, renamePath || '', currentValue.trim());
        }
      },

      content: (
        <InputComponent
          inputValue={inputValue}
          onValueChange={value => {
            currentValue = value;
          }}
        />
      )
    });
  };

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
                    createInputModal('新建文件夹', '', 'create');
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
                  <Button
                    icon={<List size={14} />}
                    onClick={() => {
                      setFileListVisible(true);
                      setUserClosedUploadList(false);
                    }}
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
                createInputModal('重命名文件', path, 'rename', path);
              }}
              onOpenFolder={path => handleFileOperation(FILE_OPERATION_TYPE.OPEN_FOLDER, path)}
              onDeleteFile={path => handleFileOperation(FILE_OPERATION_TYPE.DELETE, path)}
              onDownloadFile={(path: string, is_dir: boolean) => {
                handleFileOperation(FILE_OPERATION_TYPE.DOWNLOAD, path, '', is_dir);
              }}
            />
          </Spin>
        </Flex>
      </Card>

      {contextHolder}

      <UploadList
        fileListVisible={fileListVisible}
        currentUploadMessage={currentUploadMessage!}
        closeFileList={() => {
          setFileListVisible(false);
          setUserClosedUploadList(true);
        }}
      />
    </>
  );
};

export default File;
