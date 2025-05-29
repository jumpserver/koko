import prettyBytes from 'pretty-bytes';
import InfiniteScroll from 'react-infinite-scroll-component';

import { useEffect, useState } from 'react';
import { FileText, Download, X } from 'lucide-react';
import { Drawer, List, Progress, Divider, Typography, Flex, Skeleton, Tooltip, Card } from 'antd';

import type { UploadFileItem } from '@/types/file.type';

const { Text } = Typography;

interface UploadListProps {
  uploadFileList: any[];

  fileListVisible: boolean;

  currentUploadMessage: UploadFileItem;

  closeFileList: (visible: boolean) => void;
}

const ListDescription = ({ size, uploaded, status }: { size: number; uploaded: number; status: string }) => {
  const percent = Math.round((uploaded / size) * 100);

  let progressStatus: 'normal' | 'active' | 'success' | 'exception' = 'active';

  switch (status) {
    case 'uploading':
      progressStatus = 'active';
      break;
    case 'success':
      progressStatus = 'success';
      break;
    case 'error':
      progressStatus = 'exception';
      break;
    default:
      progressStatus = 'normal';
  }

  return (
    <div className="pr-4">
      <Progress percent={percent} size="small" status={progressStatus} />
    </div>
  );
};

const UploadList: React.FC<UploadListProps> = ({
  uploadFileList,
  fileListVisible,
  currentUploadMessage,
  closeFileList
}) => {
  const handleDownloadFile = (file: any) => {
    console.log(file);
  };
  const handleRemoveFile = (uid: string) => {
    console.log(uid);
  };

  const loadMoreData = () => {};

  useEffect(() => {
    // loadMoreData();
  }, []);

  return (
    <Drawer
      title="文件列表"
      placement="bottom"
      closeIcon={false}
      getContainer={false}
      open={fileListVisible}
      onClose={() => closeFileList(false)}
    >
      {!currentUploadMessage ? (
        <Flex vertical justify="center" align="center" className="w-full h-full">
          <FileText size={48} style={{ marginBottom: 16 }} />
          <Text>暂无文件</Text>
        </Flex>
      ) : (
        <Card id="scrollableDiv" className="h-full">
          <InfiniteScroll
            next={loadMoreData}
            loader={<div />}
            dataLength={1}
            hasMore={false}
            scrollableTarget="scrollableDiv"
          >
            <List
              bordered={false}
              dataSource={[currentUploadMessage]}
              renderItem={item => (
                <List.Item key={item.md5}>
                  <List.Item.Meta
                    title={<a href="https://ant.design">{item.filename}</a>}
                    description={
                      <ListDescription size={item.totalSize} uploaded={item.uploaded} status={item.status} />
                    }
                  />

                  <Tooltip title="删除">
                    <X
                      size={14}
                      onClick={() => handleRemoveFile(item.md5)}
                      className="cursor-pointer hover:text-red-500"
                    />
                  </Tooltip>
                </List.Item>
              )}
            />
          </InfiniteScroll>
        </Card>
      )}
    </Drawer>
  );
};

export default UploadList;
