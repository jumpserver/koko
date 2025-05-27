import './index.scss';
import { Flex, Card } from 'antd';
import { emitterEvent } from '@/utils';
import { useState, useMemo } from 'react';
import { Settings, Folder, Share2 } from 'lucide-react';

import File from '@/components/File';
import Share from '@/components/Share';
import Detail from '@/components/Detail';

import type { DrawerItem } from '@/types/sidebar.type';

const DRAWER_ITEMS: DrawerItem[] = [
  {
    label: 'Detail',
    value: 'settings',
    icon: Settings,
    component: <Detail />
  },
  {
    label: 'File',
    value: 'file',
    icon: Folder,
    component: <File />
  },
  {
    label: 'Share',
    value: 'share',
    icon: Share2,
    component: <Share />
  }
];

const DrawerTitle: React.FC = (): React.ReactNode => {
  const [activeKey, setActiveKey] = useState<string>('settings');

  const items = useMemo(() => DRAWER_ITEMS, []);

  const handleTabChange = (itemValue: string) => {
    // 如果是 file，那么需要去发送 postMessage 让 luna 生成 token
    if (itemValue === 'file') {
      emitterEvent.emit('emit-generate-file-token');
    }

    setActiveKey(itemValue);
  };

  return (
    <>
      <Flex align="center" vertical className="h-full">
        <Flex align="center" justify="space-around" className="w-full">
          {items.map(item => (
            <Flex
              align="center"
              gap="small"
              justify="center"
              className={`h-8 !pb-3 box-content icon-hover ${item.value === activeKey ? 'border-active' : ''}`}
              key={item.value}
              onClick={() => handleTabChange(item.value)}
            >
              <item.icon size={20} />
              <span>{item.label}</span>
            </Flex>
          ))}
        </Flex>

        <Flex className="w-full h-full">{items.find(item => item.value === activeKey)?.component}</Flex>
      </Flex>
    </>
  );
};

export const Sidebar: React.FC = () => {
  return (
    <>
      <Card variant="borderless" className="h-full !rounded-none !bg-[#1D1D1D]">
        <DrawerTitle />
      </Card>
    </>
  );
};
