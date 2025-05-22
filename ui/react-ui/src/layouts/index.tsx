import { Splitter } from 'antd';
import { useState } from 'react';
import { Sidebar } from '@/layouts/components/Sidebar';
import { MainContainer } from '@/layouts/components/MainContainer';

export const LayoutComponent: React.FC = () => {
  const [sidebarWidth, setSidebarWidth] = useState<number>(650);

  const handleResize = (sizes: number[]) => {
    setSidebarWidth(sizes[1]);
  };

  return (
    <Splitter onResize={handleResize} style={{ height: '100vh' }}>
      <Splitter.Panel>
        <MainContainer />
      </Splitter.Panel>

      <Splitter.Panel collapsible defaultSize={sidebarWidth} max="50%" min="30%">
        <Sidebar />
      </Splitter.Panel>
    </Splitter>
  );
};
