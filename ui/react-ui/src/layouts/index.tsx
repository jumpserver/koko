import { Splitter } from 'antd';
import { useState } from 'react';
import { emitterEvent } from '@/utils';
import { Sidebar } from '@/layouts/components/Sidebar';
import { MainContainer } from '@/layouts/components/MainContainer';

export const LayoutComponent: React.FC = () => {
  const [sidebarWidth, setSidebarWidth] = useState<number>(650);

  const handleResize = (sizes: number[]) => {
    setSidebarWidth(sizes[1]);
    emitterEvent.emit('emit-resize');
  };

  return (
    <Splitter onResize={handleResize} style={{ height: '100vh' }}>
      <Splitter.Panel style={{ overflowX: 'hidden' }}>
        <MainContainer />
      </Splitter.Panel>

      <Splitter.Panel collapsible defaultSize={sidebarWidth} max="50%" min={585} style={{ overflowX: 'hidden' }}>
        <Sidebar />
      </Splitter.Panel>
    </Splitter>
  );
};
