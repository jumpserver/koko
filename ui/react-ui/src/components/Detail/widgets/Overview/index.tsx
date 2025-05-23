import { useEffect, useState } from 'react';
import { Wifi, Cpu, MemoryStick, MoveUp, MoveDown } from 'lucide-react';
import { Descriptions, Divider, Card, Space, Flex, Popover } from 'antd';

import type { DescriptionsProps } from 'antd';

import useDetail from '@/store/useDetail';

const Overview = () => {
  const [clicked, setClicked] = useState(false);
  const [hovered, setHovered] = useState(false);
  const [items, setItems] = useState<DescriptionsProps['items']>([]);

  const { connection } = useDetail();

  const hide = () => {
    setClicked(false);
    setHovered(false);
  };

  const handleHoverChange = (open: boolean) => {
    setHovered(open);
    setClicked(false);
  };

  const handleClickChange = (open: boolean) => {
    setHovered(false);
    setClicked(open);
  };

  const handleNetworkTest = () => {};

  const clickContent = <div>This is click content.</div>;

  useEffect(() => {
    setItems([
      {
        key: '1',
        label: '登录用户',
        span: 1,
        children: connection.username
      },
      {
        key: '2',
        label: '资产地址',
        span: 1,
        children: connection.address
      },
      {
        key: '3',
        label: '资产名称',
        span: 1,
        children: connection.assetName
      }
    ]);
  }, [connection]);

  return (
    <Card variant="outlined" style={{ width: '100%', height: '100%' }}>
      <Flex className="w-full">
        <Space className="w-full justify-between" align="center" wrap={false} split={<Divider type="vertical" />}>
          <Popover content="点击进行网络延迟测试" trigger="hover" open={hovered} onOpenChange={handleHoverChange}>
            <Popover
              trigger="click"
              placement="bottom"
              open={clicked}
              content={
                <div>
                  {clickContent}
                  <a onClick={hide}>Close</a>
                </div>
              }
              onOpenChange={handleClickChange}
            >
              <Space className="min-h-8 icon-hover" onClick={handleNetworkTest}>
                <Wifi size={20} />

                <div className="flex flex-col items-center">
                  {/* TODO 颜色根据网络状况设置 */}
                  <span className="text-xxs text-primary-color">113ms</span>
                  <span className="text-xxs">RTT</span>
                </div>
              </Space>
            </Popover>
          </Popover>

          <Space className="min-h-8">
            <Cpu size={20} />

            <span className="text-xxs">1.60%</span>
          </Space>

          <Space className="min-h-8">
            <MemoryStick size={20} />

            <span className="text-xxs">38.00%</span>
          </Space>

          <Space className="min-h-8 !gap-0" direction="vertical">
            <Flex gap="small">
              <Flex align="center">
                <span className="min-w-6 text-xxs">Disk</span>
                <MoveUp size={10} />
              </Flex>

              <span className="text-xxs">0.00%</span>
            </Flex>

            <Flex gap="small">
              <Flex align="center">
                <span className="min-w-6 text-xxs">I / O</span>
                <MoveDown size={10} />
              </Flex>

              <span className="text-xxs">0.00%</span>
            </Flex>
          </Space>

          <Space className="min-h-8 !gap-0" direction="vertical">
            <Flex gap="small">
              <Flex align="center">
                <MoveUp size={10} />
                <span className="min-w-6 text-xxs">0.00</span>
              </Flex>

              <span className="text-xxs">Mbps</span>
            </Flex>

            <Flex gap="small">
              <Flex align="center">
                <MoveDown size={10} />
                <span className="min-w-6 text-xxs">0.00</span>
              </Flex>

              <span className="text-xxs">Mbps</span>
            </Flex>
          </Space>
        </Space>
      </Flex>

      <Divider dashed />

      <Descriptions title="连接信息" items={items} column={1} />
    </Card>
  );
};

export default Overview;
