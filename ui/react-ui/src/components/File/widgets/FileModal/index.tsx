import { useState } from 'react';
import { Modal, Form, Input, message } from 'antd';
import { useFileStatus } from '@/store/useFileStatus';

interface FileModalProps {
  title: string;
  visible: boolean;
  onConfirm: (fileName: string) => void;
  onCancel: () => void;
}

const FileModal: React.FC<FileModalProps> = ({ visible, title, onCancel, onConfirm }) => {
  const [inputValue, setInputValue] = useState('');

  const { fileMessage } = useFileStatus();

  const handleOk = () => {
    const isExist = fileMessage.fileList.find(item => item.name === inputValue);

    if (isExist) {
      message.error('文件名已存在');
      return;
    }

    onConfirm(inputValue);
  };

  return (
    <Modal centered open={visible} onCancel={onCancel} onOk={handleOk} title={title}>
      <Form layout="vertical">
        <Form.Item label="文件名" name="fileName">
          <Input value={inputValue} onChange={e => setInputValue(e.target.value)} />
        </Form.Item>
      </Form>
    </Modal>
  );
};

export default FileModal;
