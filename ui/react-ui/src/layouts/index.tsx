import { Splitter } from 'antd';
import { useState } from 'react';
import { Sidebar } from '@/layouts/components/Sidebar';
import { MainContainer } from '@/layouts/components/MainContainer';

export const LayoutComponent: React.FC = () => {
  const [open, setOpen] = useState<boolean>(false);
  const [sidebarWidth, setSidebarWidth] = useState<number>(650);

  const handleResize = (sizes: number[]) => {
    setSidebarWidth(sizes[1]);
  };

  return (
    <Splitter style={{ height: '100vh' }} onResize={handleResize}>
      <Splitter.Panel className="overflow-hidden">
        <MainContainer />
      </Splitter.Panel>

      {/* <Splitter.Panel defaultSize={sidebarWidth} max="50%" min="20%">
        <Sidebar open={open} setOpen={setOpen} width={sidebarWidth} />
      </Splitter.Panel> */}
    </Splitter>
  );
};
