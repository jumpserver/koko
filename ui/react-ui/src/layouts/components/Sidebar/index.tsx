import './index.scss';
import { Drawer, Flex } from 'antd';
import { useState, useMemo } from 'react';
import { Settings, Folder, Share2 } from 'lucide-react';

import type { LucideProps } from 'lucide-react';

interface DrawerItem {
  label: string;

  value: string;

  component: React.ReactNode;

  icon: React.ForwardRefExoticComponent<Omit<LucideProps, 'ref'>>;
}

interface SidebarProps {
  open: boolean;

  width: number;

  setOpen: (open: boolean) => void;
}

const DRAWER_ITEMS: DrawerItem[] = [
  {
    label: 'Detail',
    value: 'settings',
    icon: Settings,
    component: <div>Detail</div>
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
      <Flex align="center" justify="space-around" className="w-full custom-border">
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
    </>
  );
};

export const Sidebar: React.FC<SidebarProps> = ({ open, width, setOpen }) => {
  // const [loading, setLoading] = useState<boolean>(false);

  // const showLoading = () => {
  //   setOpen(true);
  //   setLoading(true);

  //   setTimeout(() => {
  //     setLoading(false);
  //   }, 2000);
  // };

  return (
    <>
      <Drawer
        title={null}
        placement="right"
        width={width}
        open={open}
        mask={false}
        closeIcon={false}
        // loading={loading}
        onClose={() => setOpen(false)}
      >
        <DrawerTitle />
      </Drawer>
    </>
  );
};
