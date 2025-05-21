import './index.scss';
import { Drawer, Flex } from 'antd';
import { useState, useMemo } from 'react';
import { Settings, Folder, Share2 } from 'lucide-react';

// import File from '@/components/File';
// import Share from '@/components/Share';
import Detail from '@/components/Detail';

import type { DrawerItem, SidebarProps } from '@/types/sidebar.type';

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
    component: <div>File</div>
  },
  {
    label: 'Share',
    value: 'share',
    icon: Share2,
    component: <div>Share</div>
  }
];

const DrawerTitle: React.FC = (): React.ReactNode => {
  const [activeKey, setActiveKey] = useState<string>('settings');

  const items = useMemo(() => DRAWER_ITEMS, []);

  const handleTabChange = (itemValue: string) => {
    setActiveKey(itemValue);
  };

  return (
    <>
      <Flex align="center" vertical className="h-full">
        <Flex align="center" justify="space-around" className="w-full custom-border">
          {items.map(item => (
            <Flex
              align="center"
              gap="small"
              justify="center"
              className={`h-8 !py-3 box-content icon-hover ${item.value === activeKey ? 'border-active' : ''}`}
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

export const Sidebar: React.FC<SidebarProps> = ({ open, width, setOpen }) => {
  return (
    <>
      <Drawer
        title={null}
        placement="right"
        width={width}
        open={open}
        mask={false}
        closeIcon={false}
        onClose={() => setOpen(false)}
      >
        <DrawerTitle />
      </Drawer>
    </>
  );
};
