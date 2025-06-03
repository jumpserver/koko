import { Upload } from 'lucide-react';
import { Upload as AntdUpload, Button } from 'antd';

import type { UploadProps, RcFile } from 'antd/es/upload/interface';

interface FileUploadProps {
  handleFileUpload: (file: RcFile) => void;
}

const FileUpload: React.FC<FileUploadProps> = ({ handleFileUpload }) => {
  const uploadProps: UploadProps = {
    name: 'single-upload',
    multiple: true,
    showUploadList: false,
    customRequest: options => {
      const { file } = options;
      const rcFile = file as RcFile;

      handleFileUpload(rcFile);
    }
  };

  return (
    <AntdUpload {...uploadProps}>
      <Button icon={<Upload size={14} />}>上传文件</Button>
    </AntdUpload>
  );
};

export default FileUpload;
